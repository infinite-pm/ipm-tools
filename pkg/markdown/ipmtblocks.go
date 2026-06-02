package markdown

import "strings"

// IPMTBlock describes a fenced ```ipmt``` code block found in a markdown source.
type IPMTBlock struct {
	Index     int      // 1-based block index within the source
	StartLine int      // 0-based line index of the opening fence
	EndLine   int      // 0-based line index of the closing fence; -1 if unterminated
	Content   string   // text between fences, joined with "\n"; no trailing newline
	Meta      []string // info-string tokens after "ipmt" (e.g. ["unresolved"]); empty for a bare fence
}

// FenceMeta returns the space-separated metadata tokens after "ipmt" in a
// ```ipmt … fence info-string (e.g. "```ipmt unresolved" → ["unresolved"]).
// Empty for a bare ```ipmt fence, or when "ipmt" is not the complete language
// token (e.g. "```ipmtX", "```ipmt-x" → nil — those are not ipmt fences).
func FenceMeta(fenceLine string) []string {
	t := strings.TrimSpace(fenceLine)
	t = strings.TrimLeft(t, "`") // drop the backticks
	rest, ok := strings.CutPrefix(t, "ipmt")
	if !ok || (rest != "" && rest[0] != ' ' && rest[0] != '\t') {
		return nil // not exactly the "ipmt" language token
	}
	return strings.Fields(rest)
}

// IsIPMTFenceStart reports whether line opens an ```ipmt``` fenced block
// (3+ backticks; tilde fences never carry ipmt). "ipmt" must be the
// complete CommonMark info-string language token, so ```ipmt and
// ```ipmt <metadata> match, but ```ipmtX and ```ipmt-x (a different
// language) do not.
//
// NOTE: a true opener also requires that no longer fence is already open
// — callers that scan whole documents must track fence state (see
// ParseFenceOpen / SkipFence) or use ScanIPMTBlocks, which does.
func IsIPMTFenceStart(line string) bool {
	f, info, ok := ParseFenceOpen(line)
	return ok && f.Char == '`' && IsIPMTInfo(info)
}

// NormalizeLF rewrites all line endings in s to "\n".
func NormalizeLF(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return s
}

// ScanIPMTBlocks splits text into LF-normalized lines and extracts all
// ```ipmt``` fenced code blocks. Unterminated blocks have EndLine == -1
// and their Content holds everything after the opening fence.
//
// CommonMark-faithful on nesting (see fence.go): a ```ipmt line inside
// another fenced block — e.g. a ````md documentation example — is literal
// text, NOT a block; such wrapper fences are skipped wholesale. An ipmt
// block's closing fence must be at least as long as its opener with no
// info string, so a ```text line inside the block stays content.
func ScanIPMTBlocks(text string) (lines []string, blocks []IPMTBlock) {
	lines = strings.Split(NormalizeLF(text), "\n")
	idx := 0
	for i := 0; i < len(lines); i++ {
		f, info, ok := ParseFenceOpen(lines[i])
		if !ok {
			continue
		}
		if f.Char != '`' || !IsIPMTInfo(info) {
			// Some other fenced block (a ````md example wrapper, ```go,
			// ~~~text, …): its content is literal. Jump past it; when it
			// never closes, the rest of the document is inside it.
			i = SkipFence(lines, i, f) - 1 // -1: loop's i++ lands just past the closer
			continue
		}
		idx++
		end := -1
		for j := i + 1; j < len(lines); j++ {
			if f.ClosedBy(lines[j]) {
				end = j
				break
			}
		}
		var content string
		if end == -1 {
			content = strings.Join(lines[i+1:], "\n")
		} else {
			content = strings.Join(lines[i+1:end], "\n")
		}
		blocks = append(blocks, IPMTBlock{
			Index:     idx,
			StartLine: i,
			EndLine:   end,
			Content:   content,
			Meta:      FenceMeta(lines[i]),
		})
		if end == -1 {
			break
		}
		i = end
	}
	return lines, blocks
}
