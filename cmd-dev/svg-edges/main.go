package main

// Command svg-edges reports the geometry of each rendered edge segment in an
// .ipm.svg file: which edges are vertical, horizontal, or tilted, and by how
// much. It replaces the ad-hoc `grep '<line'` one-liners used during the
// uniform-edge-treatment work.
//
// The renderer emits one group per edge:
//
//	<g class="edge" data-edge-idx data-edge-base data-edge-from data-edge-to>
//	  <line x1 y1 x2 y2 .../>      (one or more straight segments)
//	  <path d="M … Z" fill .../>   (the arrowhead — ignored here)
//	</g>
//
// Only <line> elements are classified; the arrowhead <path> is skipped. Each
// segment is tagged V (|dx|<0.5), H (|dy|<0.5), or its angle from the vertical
// axis in degrees (a diagonal fan-out edge).
//
// Usage:
//
//	go run ./cmd-dev/svg-edges [flags] <file.ipm.svg|dir>...
//
// A dir is searched recursively for *.ipm.svg. With no args nothing is read.
// When a sibling <stem>.layout.json exists, --labels swaps the numeric node
// ids in the FROM/TO columns for node labels.
//
// Flags:
//
//	--base <base>     only edges with this data-edge-base (leadsto, expresses, …)
//	--edge <from>,<to>  only the single edge between these node ids
//	--min-angle N     only print tilted segments at >= N degrees from vertical
//	--summary         per-file histogram (#V #H #tilted, max/mean tilt) only
//	--labels          show node labels (from sibling .layout.json) not ids
//
// Related: gl:cmd/ipmsvg-gen/main.go gl:pkg/render gl:cmd/layout-gen/main.go

import (
	"encoding/json"
	"encoding/xml"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/infinite-pm/ipm-tools/pkg/cli"
)

// straightTol is the |dx|/|dy| pixel slack below which a segment counts as
// perfectly axis-aligned. The renderer emits ports at 0.01px precision, so a
// genuinely straight edge lands well inside half a pixel.
const straightTol = 0.5

type segment struct {
	file    string
	edgeIdx string
	base    string
	from    string
	to      string
	x1, y1  float64
	x2, y2  float64
}

func (s segment) dx() float64 { return s.x2 - s.x1 }
func (s segment) dy() float64 { return s.y2 - s.y1 }
func (s segment) length() float64 {
	return math.Hypot(s.dx(), s.dy())
}

// isV / isH report perfect axis alignment within straightTol.
func (s segment) isV() bool { return math.Abs(s.dx()) < straightTol }
func (s segment) isH() bool { return math.Abs(s.dy()) < straightTol }

// angleFromVertical is the segment's deviation from a vertical line, in
// degrees (0 = vertical, 90 = horizontal). Meaningful for tilted segments.
func (s segment) angleFromVertical() float64 {
	return math.Atan2(math.Abs(s.dx()), math.Abs(s.dy())) * 180 / math.Pi
}

// class is the printed classification: "V", "H", or the angle from vertical.
func (s segment) class() string {
	switch {
	case s.isV():
		return "V"
	case s.isH():
		return "H"
	default:
		return fmt.Sprintf("%.1f°", s.angleFromVertical())
	}
}

func (s segment) tilted() bool { return !s.isV() && !s.isH() }

func main() {
	base := flag.String("base", "", "only edges with this data-edge-base (e.g. leadsto, expresses)")
	edge := flag.String("edge", "", "only the single edge `from,to` (node ids)")
	minAngle := flag.Float64("min-angle", 0, "only tilted segments at >= this angle from vertical (degrees)")
	summary := flag.Bool("summary", false, "print a per-file histogram instead of the segment table")
	labels := flag.Bool("labels", false, "show node labels (from sibling .layout.json) instead of node ids")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: svg-edges [flags] <file.ipm.svg|dir>...")
		fmt.Fprintln(os.Stderr, "Classify rendered edge segments (V / H / tilted) in .ipm.svg files.")
		fmt.Fprintln(os.Stderr, "\nFlags:")
		cli.PrintDefaults(flag.CommandLine, os.Stderr)
	}
	flag.Parse()

	if flag.NArg() == 0 {
		flag.Usage()
		os.Exit(2)
	}

	var edgeFrom, edgeTo string
	if *edge != "" {
		parts := strings.SplitN(*edge, ",", 2)
		if len(parts) != 2 {
			fmt.Fprintf(os.Stderr, "svg-edges: --edge wants from,to (got %q)\n", *edge)
			os.Exit(2)
		}
		edgeFrom, edgeTo = strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	}

	files, err := collectSVGFiles(flag.Args())
	if err != nil {
		fmt.Fprintf(os.Stderr, "svg-edges: %v\n", err)
		os.Exit(2)
	}
	if len(files) == 0 {
		fmt.Fprintln(os.Stderr, "svg-edges: no .ipm.svg files found")
		os.Exit(2)
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	if *summary {
		fmt.Fprintln(tw, "FILE\tSEGS\tV\tH\tTILTED\tMAX°\tMEAN°")
	} else {
		fmt.Fprintln(tw, "FILE\tEDGE\tBASE\tFROM\tTO\tDX\tDY\tLEN\tCLASS")
	}

	exitCode := 0
	for _, file := range files {
		segs, err := parseSVGEdges(file)
		if err != nil {
			fmt.Fprintf(os.Stderr, "svg-edges: %s: %v\n", file, err)
			exitCode = 2
			continue
		}
		var lookup map[string]string
		if *labels {
			lookup = loadLabelLookup(file)
		}
		segs = filterSegments(segs, *base, edgeFrom, edgeTo, *minAngle)
		if *summary {
			printSummary(tw, file, segs)
			continue
		}
		for _, s := range segs {
			from, to := s.from, s.to
			if lookup != nil {
				from = labelOr(lookup, s.from)
				to = labelOr(lookup, s.to)
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%.1f\t%.1f\t%.1f\t%s\n",
				file, s.edgeIdx, s.base, from, to, s.dx(), s.dy(), s.length(), s.class())
		}
	}
	tw.Flush()
	os.Exit(exitCode)
}

// filterSegments applies the --base / --edge / --min-angle filters. --min-angle
// keeps only tilted segments (V and H are never "tilted" and are dropped once a
// positive threshold is set).
func filterSegments(segs []segment, base, from, to string, minAngle float64) []segment {
	out := segs[:0:0]
	for _, s := range segs {
		if base != "" && s.base != base {
			continue
		}
		if from != "" && (s.from != from || s.to != to) {
			continue
		}
		if minAngle > 0 && (!s.tilted() || s.angleFromVertical() < minAngle) {
			continue
		}
		out = append(out, s)
	}
	return out
}

func printSummary(tw io.Writer, file string, segs []segment) {
	nV, nH, nTilted := 0, 0, 0
	maxA, sumA := 0.0, 0.0
	for _, s := range segs {
		switch {
		case s.isV():
			nV++
		case s.isH():
			nH++
		default:
			nTilted++
			a := s.angleFromVertical()
			sumA += a
			if a > maxA {
				maxA = a
			}
		}
	}
	meanA := 0.0
	if nTilted > 0 {
		meanA = sumA / float64(nTilted)
	}
	fmt.Fprintf(tw, "%s\t%d\t%d\t%d\t%d\t%.1f\t%.1f\n",
		file, len(segs), nV, nH, nTilted, maxA, meanA)
}

// --- SVG parsing -----------------------------------------------------------

// parseSVGEdges streams the SVG and returns one segment per <line> inside each
// <g class="edge">. The arrowhead <path> and all non-edge groups are ignored.
// Nested groups inside an edge (none today, but cheap insurance) are tracked by
// depth so the edge context closes on its own </g>.
func parseSVGEdges(file string) ([]segment, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	dec := xml.NewDecoder(f)
	var segs []segment
	var cur *segment // current edge group (from its data-* attrs), nil outside
	depth := 0       // <g> nesting depth inside the current edge group

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "g":
				if cur != nil {
					depth++
					continue
				}
				if attr(t, "class") == "edge" {
					cur = &segment{
						edgeIdx: attr(t, "data-edge-idx"),
						base:    attr(t, "data-edge-base"),
						from:    attr(t, "data-edge-from"),
						to:      attr(t, "data-edge-to"),
					}
					depth = 0
				}
			case "line":
				if cur == nil {
					continue
				}
				s := *cur
				s.file = file
				s.x1 = atof(attr(t, "x1"))
				s.y1 = atof(attr(t, "y1"))
				s.x2 = atof(attr(t, "x2"))
				s.y2 = atof(attr(t, "y2"))
				segs = append(segs, s)
			}
		case xml.EndElement:
			if t.Name.Local == "g" && cur != nil {
				if depth == 0 {
					cur = nil
				} else {
					depth--
				}
			}
		}
	}
	return segs, nil
}

func attr(e xml.StartElement, name string) string {
	for _, a := range e.Attr {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}

func atof(s string) float64 {
	v, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return v
}

// --- node-id → label lookup (optional) -------------------------------------

// loadLabelLookup reads the sibling <stem>.layout.json (foo.ipm.svg →
// foo.layout.json) and maps node id → label. Returns nil when absent or
// unreadable — label lookup is a convenience, never a hard dependency.
func loadLabelLookup(svgFile string) map[string]string {
	stem := strings.TrimSuffix(svgFile, ".ipm.svg")
	if stem == svgFile {
		stem = strings.TrimSuffix(svgFile, filepath.Ext(svgFile))
	}
	data, err := os.ReadFile(stem + ".layout.json")
	if err != nil {
		return nil
	}
	var g struct {
		Nodes []struct {
			ID    string `json:"id"`
			Label string `json:"label"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(data, &g); err != nil {
		return nil
	}
	m := make(map[string]string, len(g.Nodes))
	for _, n := range g.Nodes {
		m[n.ID] = strings.ReplaceAll(n.Label, "\n", "\\n")
	}
	return m
}

func labelOr(lookup map[string]string, id string) string {
	if l, ok := lookup[id]; ok && l != "" {
		return l
	}
	return id
}

// --- file collection -------------------------------------------------------

func collectSVGFiles(args []string) ([]string, error) {
	var files []string
	for _, arg := range args {
		info, err := os.Stat(arg)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			files = append(files, arg)
			continue
		}
		err = filepath.WalkDir(arg, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() && strings.HasSuffix(path, ".ipm.svg") {
				files = append(files, path)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Strings(files)
	return files, nil
}
