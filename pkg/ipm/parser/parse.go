package parser

import (
	"fmt"
	"strings"

	"github.com/infinite-pm/ipm-tools/pkg/ipm/model"
)

// effEdgeType returns the kind used for edge inference and type-pair validation:
// an Unresolved node is taken as its primary candidate (Candidates[0]); any
// other node keeps its Type. The node's stored Type stays Unresolved (it renders
// grey); only edge reasoning uses the primary — matching the solver, layout, and
// validator. So `event --::X--> n ::?ct` is read as event → concept (valid).
func effEdgeType(n model.Node) model.NodeType {
	if n.Type == model.Unresolved && len(n.Candidates) > 0 {
		return n.Candidates[0]
	}
	return n.Type
}

// Parse parses ipmt source into an IpmGraph.
// It returns an error (possibly *ParseError with source positions) on failure.
func Parse(input []byte, opt Options) (*model.IpmGraph, error) {
	raw := string(input)

	// Empty input → empty graph
	if strings.TrimSpace(raw) == "" {
		return &model.IpmGraph{
			Version: "25.09",
			Src: model.Src{
				Type:      "ipmt",
				Ipmt:      raw,
				Positions: model.Positions{Nodes: map[int][2]int{}, Edges: map[int]model.EdgePos{}},
			},
		}, nil
	}

	// Phase 1: Preprocess
	lines, comments, err := preprocess(raw)
	if err != nil {
		return nil, err
	}

	if len(lines) == 0 {
		return &model.IpmGraph{
			Version: "25.09",
			Src: model.Src{
				Type:      "ipmt",
				Ipmt:      raw,
				Positions: model.Positions{Nodes: map[int][2]int{}, Edges: map[int]model.EdgePos{}},
				Lex:       model.LexSpans{Comments: comments},
			},
		}, nil
	}

	// Graph builder state
	var nodes []model.Node
	var edges []model.Edge
	var eposes []model.EdgePos
	pos := model.Positions{Nodes: map[int][2]int{}, Edges: map[int]model.EdgePos{}}

	// Transient lexeme spans for the semantic tokenizer (see model.Src.Lex).
	lex := model.LexSpans{Comments: comments}

	// Every node-name occurrence (raw span + the name as written), so a
	// post-pass can flag the ones that resolved to an alias — aliases may be
	// used before they're defined, so the match can't be made inline.
	type nameOccurrence struct {
		name string
		span [2]int
	}
	var occurrences []nameOccurrence

	// collectSpecLex maps a node spec's logical-line lexeme spans to raw byte
	// offsets and appends them to lex. Called once per spec occurrence (at
	// the parseNodeSpecs sites, not per-edge — a segment shared by two arrows
	// must not double-count).
	collectSpecLex := func(specs []NodeSpec, ll LogicalLine) {
		mapSpan := func(s [2]int) ([2]int, bool) {
			ns, ne := s[0], s[1]
			if ns < 0 || ne <= ns || ns >= len(ll.M2R) || ne-1 >= len(ll.M2R) {
				return [2]int{}, false
			}
			return [2]int{ll.M2R[ns], ll.M2R[ne-1] + 1}, true
		}
		for _, spec := range specs {
			if r, ok := mapSpan([2]int{spec.NameStart, spec.NameEnd}); ok {
				occurrences = append(occurrences, nameOccurrence{name: spec.Name, span: r})
			}
			if r, ok := mapSpan(spec.KindMarker); ok {
				lex.KindMarkers = append(lex.KindMarkers, model.KindSpan{Span: r, Type: spec.Type})
			}
			if r, ok := mapSpan(spec.AliasMark); ok {
				lex.TypeMarkers = append(lex.TypeMarkers, r)
			}
			if r, ok := mapSpan(spec.TipMark); ok {
				lex.TypeMarkers = append(lex.TypeMarkers, r)
			}
			if r, ok := mapSpan(spec.AliasName); ok {
				lex.AliasDecls = append(lex.AliasDecls, model.KindSpan{Span: r, Type: spec.Type})
			}
			if r, ok := mapSpan(spec.TipString); ok {
				lex.Tooltips = append(lex.Tooltips, r)
			}
			if spec.NameQuoted {
				if r, ok := mapSpan([2]int{spec.NameStart, spec.NameEnd}); ok {
					lex.Strings = append(lex.Strings, r)
				}
			}
			// ::?<letters> — emit the `::?` prefix as the dedicated
			// ipmUnresolved (neutral grey) marker, and each candidate letter
			// as a kind marker of its own kind, so `::?etc` renders as `::?`
			// (unresolved) + `e` (event) + `t` (thing) + `c` (concept).
			if r, ok := mapSpan(spec.UndecidedPrefix); ok {
				lex.UnresolvedPrefixes = append(lex.UnresolvedPrefixes, r)
			}
			for _, l := range spec.UndecidedLetters {
				if r, ok := mapSpan(l.Span); ok {
					lex.KindMarkers = append(lex.KindMarkers, model.KindSpan{Span: r, Type: l.Kind})
				}
			}
		}
	}

	// Track seen edge pairs for duplicate detection
	pairs := make(map[string]struct{})
	pairKey := func(a, b int, t model.SstLinkType) string {
		return fmt.Sprintf("%d:%d:%s", a, b, t)
	}
	// Track the SST relation type per UNORDERED node pair: the four base relations
	// are mutually exclusive per pair. (The validator also enforces this — IPMV1.2 —
	// for graphs that come from other sources, not just the parser.)
	pairTypes := make(map[string]model.SstLinkType)
	unorderedPairKey := func(a, b int) string {
		if a > b {
			a, b = b, a
		}
		return fmt.Sprintf("%d:%d", a, b)
	}

	// nameIdx / aliasIdx index nodes by Name and (non-empty) Alias so node
	// lookup is O(1) instead of a linear scan per spec (which made parsing
	// O(n^2) in node count). They mirror the four match conditions of the old
	// scan: a spec can resolve against either its Name or its Alias, matched
	// against either a node's Name or Alias. When more than one key matches the
	// lowest node index wins, preserving the first-match-wins order of the
	// original linear scan. BOTH indexes key only on NON-EMPTY values: an empty
	// name is no identity at all, so alias-only declarations
	// (`alpha::a ::?etc`, `beta::a ::?etc`) must stay distinct nodes instead of
	// collapsing into the first one and swallowing its edges.
	nameIdx := make(map[string]int)  // Name -> lowest node index
	aliasIdx := make(map[string]int) // Alias -> lowest node index (non-empty only)
	register := func(m map[string]int, key string, idx int) {
		if prev, ok := m[key]; !ok || idx < prev {
			m[key] = idx
		}
	}

	// lookupOrCreateNode finds an existing node by name/alias or creates a new one.
	lookupOrCreateNode := func(spec NodeSpec, ll LogicalLine) (int, model.NodeType) {
		// Lookup by name or alias. Mirror the old linear scan's first-match-wins
		// ordering by choosing the lowest matching node index.
		bestIdx := -1
		consider := func(idx int, ok bool) {
			if ok && (bestIdx < 0 || idx < bestIdx) {
				bestIdx = idx
			}
		}
		// cond1: node.Name == spec.Name; cond2: node.Alias == spec.Name.
		// An empty spec.Name matches nothing (it is not an identity).
		if spec.Name != "" {
			i1, ok1 := nameIdx[spec.Name]
			consider(i1, ok1)
			i2, ok2 := aliasIdx[spec.Name]
			consider(i2, ok2)
		}
		if spec.Alias != "" {
			// cond3: node.Alias == spec.Alias; cond4: node.Name == spec.Alias.
			i3, ok3 := aliasIdx[spec.Alias]
			consider(i3, ok3)
			i4, ok4 := nameIdx[spec.Alias]
			consider(i4, ok4)
		}
		if bestIdx >= 0 {
			nd := &nodes[bestIdx]
			return nd.ID, effEdgeType(*nd)
		}
		// Create new node
		id := len(nodes) + 1
		nd := model.Node{
			ID:         id,
			Name:       spec.Name,
			Alias:      spec.Alias,
			Type:       spec.Type,
			Candidates: spec.Candidates,
			Tooltip:    spec.Tooltip,
		}
		newIdx := len(nodes)
		nodes = append(nodes, nd)
		if nd.Name != "" {
			register(nameIdx, nd.Name, newIdx)
		}
		if nd.Alias != "" {
			register(aliasIdx, nd.Alias, newIdx)
		}

		// Map position from logical line to raw
		ns := spec.NameStart
		ne := spec.NameEnd
		if ns < len(ll.M2R) && ne-1 < len(ll.M2R) && ne > ns {
			rawStart := ll.M2R[ns]
			rawEnd := ll.M2R[ne-1] + 1
			pos.Nodes[id] = [2]int{rawStart, rawEnd}
		}
		return id, effEdgeType(nd)
	}

	// inferSst infers the SST link type from source/target node types.
	inferSst := func(st, tt model.NodeType) model.SstLinkType {
		if st == model.Event && tt == model.Event {
			return model.LeadsTo
		}
		if st == model.Concept || tt == model.Concept {
			return model.Expresses
		}
		return model.PartOf
	}

	// validateEdgeDirection checks if the source→target type combination is valid.
	validateEdgeDirection := func(st, tt model.NodeType, isUndirected bool) string {
		if isUndirected {
			if st != tt {
				return fmt.Sprintf("invalid edge: undirected (---) only allowed between same types, got %s --- %s", st, tt)
			}
			return ""
		}
		if st == model.Concept && tt == model.Event {
			return "invalid edge direction: concept → event is not valid; only event → concept is allowed"
		}
		if st == model.Concept && tt == model.Thing {
			return "invalid edge direction: concept → thing is not valid"
		}
		if st == model.Event && tt == model.Thing {
			return "invalid edge direction: part-of is thing → event, not event → thing; flip the arrow"
		}
		return ""
	}

	// validateExplicitEdge checks if the explicit SST type is valid for the source→target combination.
	validateExplicitEdge := func(st, tt model.NodeType, sst model.SstLinkType) string {
		switch sst {
		case model.LeadsTo:
			if st != model.Event || tt != model.Event {
				return fmt.Sprintf("invalid edge: LeadsTo (::L) only valid for event → event, got %s → %s", st, tt)
			}
		case model.PartOf:
			validPartOf := (st == model.Event && tt == model.Event) ||
				(st == model.Thing && tt == model.Event) ||
				(st == model.Thing && tt == model.Thing)
			if !validPartOf {
				return fmt.Sprintf("invalid edge: PartOf (::P) only valid for event→event, thing→event, thing→thing, got %s → %s", st, tt)
			}
		case model.Expresses:
			validExpresses := (st == model.Event && tt == model.Event) ||
				(st == model.Event && tt == model.Concept) ||
				(st == model.Thing && tt == model.Concept) ||
				(st == model.Concept && tt == model.Concept)
			if !validExpresses {
				return fmt.Sprintf("invalid edge: Expresses (::X) only valid for event→event, event→concept, thing→concept, concept→concept, got %s → %s", st, tt)
			}
		case model.NearTo:
			if st != tt {
				return fmt.Sprintf("invalid edge: NearTo (::N) only valid for same types, got %s --- %s", st, tt)
			}
		}
		return ""
	}

	addEdge := func(sid, tid int, dir model.ArrowDir, sst model.SstLinkType, tip string,
		arrowRawS, arrowRawE int, srcRaw, tgtRaw [2]int) error {
		k := pairKey(sid, tid, sst)
		if _, exists := pairs[k]; exists {
			return &ParseError{
				Msg:   fmt.Sprintf("invalid syntax: duplicate edge between same nodes (pair %d-%d)", sid, tid),
				Start: arrowRawS,
				End:   arrowRawE,
			}
		}
		pairs[k] = struct{}{}
		upk := unorderedPairKey(sid, tid)
		if prev, exists := pairTypes[upk]; exists && prev != sst {
			return &ParseError{
				Msg:   fmt.Sprintf("invalid syntax: conflicting SST relations between nodes %d and %d (%s and %s); the four base relations are mutually exclusive per pair", sid, tid, prev, sst),
				Start: arrowRawS,
				End:   arrowRawE,
			}
		}
		pairTypes[upk] = sst
		edges = append(edges, model.Edge{
			ID:          0,
			Source:      sid,
			Target:      tid,
			Dir:         dir,
			Tooltip:     tip,
			SstLinkType: sst,
			SemOrigin:   model.SemInferred,
		})
		eposes = append(eposes, model.EdgePos{
			Arrow:  [2]int{arrowRawS, arrowRawE},
			Source: srcRaw,
			Target: tgtRaw,
		})
		return nil
	}

	// Process each logical line
	for _, ll := range lines {
		// Phase 2: Tokenize
		toks, err := tokenize(ll.Text)
		if err != nil {
			// Convert scanError to ParseError with raw positions
			if se, ok := err.(*scanError); ok {
				rs := se.start
				re := se.end
				if rs < len(ll.M2R) && re-1 < len(ll.M2R) {
					return nil, &ParseError{Msg: se.msg, Start: ll.M2R[rs], End: ll.M2R[re-1] + 1}
				}
				return nil, &ParseError{Msg: se.msg, Start: 0, End: 0}
			}
			return nil, err
		}

		// Validate token patterns
		if err := validateTokens(ll.Text, toks); err != nil {
			return nil, err
		}

		// Separate arrow tokens and node segments
		arrows := arrowTokens(toks)

		if len(arrows) == 0 {
			// Node-only line: parse all node segments
			text := strings.TrimSpace(ll.Text)
			if text == "" {
				continue
			}
			specs, err := parseNodeSpecs(text, strings.Index(ll.Text, text))
			if err != nil {
				return nil, err
			}
			collectSpecLex(specs, ll)
			for _, spec := range specs {
				lookupOrCreateNode(spec, ll)
			}
			continue
		}

		// Edge line: build segments between arrows
		type segment struct {
			start, end int // positions in ll.Text
		}
		var segs []segment
		last := 0
		for _, a := range arrows {
			segs = append(segs, segment{last, a.Start})
			last = a.End
		}
		segs = append(segs, segment{last, len(ll.Text)})

		// Parse each segment into node specs
		segSpecs := make([][]NodeSpec, len(segs))
		for si, seg := range segs {
			text := ll.Text[seg.start:seg.end]
			if strings.TrimSpace(text) == "" {
				continue
			}
			specs, err := parseNodeSpecs(text, seg.start)
			if err != nil {
				return nil, err
			}
			collectSpecLex(specs, ll)
			segSpecs[si] = specs
		}

		// Validate no multi-source × multi-target
		for i := range arrows {
			leftSpecs := segSpecs[i]
			rightSpecs := segSpecs[i+1]
			if len(leftSpecs) > 1 && len(rightSpecs) > 1 {
				return nil, fmt.Errorf("invalid syntax: multiple sources and multiple targets in one segment: %q", ll.Text)
			}
		}

		// Emit edges for each arrow
		for ai, arrow := range arrows {
			leftSpecs := segSpecs[ai]
			rightSpecs := segSpecs[ai+1]

			// Resolve nodes
			type resolvedNode struct {
				id  int
				typ model.NodeType
				raw [2]int // raw source position for this edge reference
			}
			resolveSpecs := func(specs []NodeSpec) []resolvedNode {
				var rn []resolvedNode
				for _, spec := range specs {
					id, typ := lookupOrCreateNode(spec, ll)
					// Use the current-line position for the edge source/target,
					// not the first-seen position stored in pos.Nodes
					var nraw [2]int
					ns := spec.NameStart
					ne := spec.NameEnd
					if ns < len(ll.M2R) && ne > 0 && ne-1 < len(ll.M2R) {
						nraw = [2]int{ll.M2R[ns], ll.M2R[ne-1] + 1}
					} else {
						nraw = pos.Nodes[id]
					}
					rn = append(rn, resolvedNode{id: id, typ: typ, raw: nraw})
				}
				return rn
			}

			lefts := resolveSpecs(leftSpecs)
			rights := resolveSpecs(rightSpecs)

			// Map arrow positions to raw
			arS := arrow.Start
			arE := arrow.End
			var arrowRawS, arrowRawE int
			if arS < len(ll.M2R) && arE-1 < len(ll.M2R) {
				arrowRawS = ll.M2R[arS]
				arrowRawE = ll.M2R[arE-1] + 1
			}

			switch arrow.Kind {
			case TokArrowFwd:
				for _, l := range lefts {
					for _, r := range rights {
						if errMsg := validateEdgeDirection(l.typ, r.typ, false); errMsg != "" {
							return nil, &ParseError{Msg: errMsg, Start: arrowRawS, End: arrowRawE}
						}
						sst := inferSst(l.typ, r.typ)
						// part-of direction is thing→event; event→thing is rejected by
						// validateEdgeDirection above (no silent auto-inversion).
						if err := addEdge(l.id, r.id, model.DirFwd, sst, "", arrowRawS, arrowRawE, l.raw, r.raw); err != nil {
							return nil, err
						}
					}
				}
			case TokArrowRev:
				for _, l := range lefts {
					for _, r := range rights {
						if errMsg := validateEdgeDirection(r.typ, l.typ, false); errMsg != "" {
							return nil, &ParseError{Msg: errMsg, Start: arrowRawS, End: arrowRawE}
						}
						sst := inferSst(r.typ, l.typ)
						if err := addEdge(r.id, l.id, model.DirFwd, sst, "", arrowRawS, arrowRawE, r.raw, l.raw); err != nil {
							return nil, err
						}
					}
				}
			case TokArrowUndir:
				for _, l := range lefts {
					for _, r := range rights {
						if errMsg := validateEdgeDirection(l.typ, r.typ, true); errMsg != "" {
							return nil, &ParseError{Msg: errMsg, Start: arrowRawS, End: arrowRawE}
						}
						if err := addEdge(l.id, r.id, model.DirUndir, model.NearTo, "", arrowRawS, arrowRawE, l.raw, r.raw); err != nil {
							return nil, err
						}
					}
				}
			case TokArrowExpl:
				mapExp := map[byte]model.SstLinkType{
					'P': model.PartOf,
					'X': model.Expresses,
					'L': model.LeadsTo,
					'N': model.NearTo,
				}
				sst, ok := mapExp[arrow.ExpCode]
				if !ok {
					return nil, &ParseError{
						Msg:   fmt.Sprintf("invalid syntax: unknown explicit edge type ::%c", arrow.ExpCode),
						Start: arrowRawS, End: arrowRawE,
					}
				}
				dir := model.DirFwd
				if arrow.ExpCode == 'N' || arrow.Undir {
					dir = model.DirUndir
				}
				for _, l := range lefts {
					for _, r := range rights {
						if arrow.Reverse {
							if errMsg := validateExplicitEdge(r.typ, l.typ, sst); errMsg != "" {
								return nil, &ParseError{Msg: errMsg, Start: arrowRawS, End: arrowRawE}
							}
							if err := addEdge(r.id, l.id, dir, sst, arrow.Tooltip, arrowRawS, arrowRawE, r.raw, l.raw); err != nil {
								return nil, err
							}
						} else {
							if errMsg := validateExplicitEdge(l.typ, r.typ, sst); errMsg != "" {
								return nil, &ParseError{Msg: errMsg, Start: arrowRawS, End: arrowRawE}
							}
							if err := addEdge(l.id, r.id, dir, sst, arrow.Tooltip, arrowRawS, arrowRawE, l.raw, r.raw); err != nil {
								return nil, err
							}
						}
					}
				}
			case TokArrowTooltip:
				for _, l := range lefts {
					for _, r := range rights {
						if arrow.Undir {
							if errMsg := validateEdgeDirection(l.typ, r.typ, true); errMsg != "" {
								return nil, &ParseError{Msg: errMsg, Start: arrowRawS, End: arrowRawE}
							}
							if err := addEdge(l.id, r.id, model.DirUndir, model.NearTo, arrow.Tooltip, arrowRawS, arrowRawE, l.raw, r.raw); err != nil {
								return nil, err
							}
						} else if arrow.Reverse {
							if errMsg := validateEdgeDirection(r.typ, l.typ, false); errMsg != "" {
								return nil, &ParseError{Msg: errMsg, Start: arrowRawS, End: arrowRawE}
							}
							sst := inferSst(r.typ, l.typ)
							if err := addEdge(r.id, l.id, model.DirFwd, sst, arrow.Tooltip, arrowRawS, arrowRawE, r.raw, l.raw); err != nil {
								return nil, err
							}
						} else {
							if errMsg := validateEdgeDirection(l.typ, r.typ, false); errMsg != "" {
								return nil, &ParseError{Msg: errMsg, Start: arrowRawS, End: arrowRawE}
							}
							sst := inferSst(l.typ, r.typ)
							if err := addEdge(l.id, r.id, model.DirFwd, sst, arrow.Tooltip, arrowRawS, arrowRawE, l.raw, r.raw); err != nil {
								return nil, err
							}
						}
					}
				}
			}
		}
	}

	// Assign stable edge IDs starting at N+1
	if len(edges) > 0 {
		pos.Edges = make(map[int]model.EdgePos, len(edges))
		base := len(nodes) + 1
		for i := range edges {
			edges[i].ID = base + i
			pos.Edges[edges[i].ID] = eposes[i]
		}
	}

	// Alias references: name occurrences that resolved to a known alias.
	// Done now (not inline) because an alias may be used before it's
	// defined. Edge-endpoint refs are also colored by the tokenizer's
	// endpoint pass; the identical tokens dedupe, so emit them all.
	if len(occurrences) > 0 {
		aliasKind := map[string]model.NodeType{}
		for _, n := range nodes {
			if n.Alias != "" {
				aliasKind[n.Alias] = n.Type
			}
		}
		for _, occ := range occurrences {
			if typ, ok := aliasKind[occ.name]; ok {
				lex.AliasRefs = append(lex.AliasRefs, model.KindSpan{Span: occ.span, Type: typ})
			}
		}
	}

	doc := &model.IpmGraph{
		Version: "25.09",
		Src:     model.Src{Type: "ipmt", Ipmt: raw, Positions: pos, Lex: lex},
		Nodes:   nodes,
		Edges:   edges,
	}
	return doc, nil
}
