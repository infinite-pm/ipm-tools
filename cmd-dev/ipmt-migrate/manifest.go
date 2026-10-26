package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/infinite-pm/ipm-tools/pkg/markdown"
)

// Manifest kinds.
const (
	KindFile  = "file"
	KindFence = "fence"
	KindLine  = "line"
)

// Entry is one manifest place.
type Entry struct {
	Kind   string
	Path   string
	Anchor string // fence: one line of the fence
	Text   string // line: the exact line
	E, T   int    // minimum counts (file, fence)
	Why    string
	Src    int // manifest line number
}

// Manifest is the parsed ipmt-markers.manifest.
type Manifest struct {
	Rule    string // "thing" or "event"
	Entries []Entry
}

var (
	reCounts  = regexp.MustCompile(`^e=(\d+) t=(\d+)$`)
	reMarkE   = regexp.MustCompile(`::e\b`)
	reMarkT   = regexp.MustCompile(`::t\b`)
	reAnyMark = regexp.MustCompile(`::[etc]\b`)
)

// ParseManifest parses the manifest text: TAB-separated, '#' comment lines and
// blank lines ignored, the first data line is "rule<TAB>thing|event".
func ParseManifest(data []byte) (*Manifest, error) {
	m := &Manifest{}
	for i, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimRight(raw, "\r")
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		bad := func(format string, a ...any) error {
			return fmt.Errorf("manifest line %d: %s", i+1, fmt.Sprintf(format, a...))
		}
		if m.Rule == "" {
			if len(f) != 2 || f[0] != "rule" || (f[1] != "thing" && f[1] != "event") {
				return nil, bad("the first data line must be \"rule<TAB>thing\" or \"rule<TAB>event\"")
			}
			m.Rule = f[1]
			continue
		}
		e := Entry{Kind: f[0], Src: i + 1}
		counts := func(s string) error {
			c := reCounts.FindStringSubmatch(s)
			if c == nil {
				return bad("counts must read \"e=N t=N\", got %q", s)
			}
			e.E, _ = strconv.Atoi(c[1])
			e.T, _ = strconv.Atoi(c[2])
			return nil
		}
		var why string
		switch f[0] {
		case KindFile:
			if len(f) != 4 {
				return nil, bad("file entry needs 4 fields (file, path, counts, why), has %d", len(f))
			}
			e.Path, why = f[1], f[3]
			if err := counts(f[2]); err != nil {
				return nil, err
			}
		case KindFence:
			if len(f) != 5 {
				return nil, bad("fence entry needs 5 fields (fence, path, anchor, counts, why), has %d", len(f))
			}
			e.Path, e.Anchor, why = f[1], f[2], f[4]
			if e.Anchor == "" {
				return nil, bad("fence anchor is empty")
			}
			if err := counts(f[3]); err != nil {
				return nil, err
			}
		case KindLine:
			if len(f) != 4 {
				return nil, bad("line entry needs 4 fields (line, path, text, why), has %d", len(f))
			}
			e.Path, e.Text, why = f[1], f[2], f[3]
			if !reAnyMark.MatchString(e.Text) {
				return nil, bad("line text must contain ::e, ::t or ::c")
			}
		default:
			return nil, bad("unknown entry kind %q", f[0])
		}
		if e.Path == "" {
			return nil, bad("empty path")
		}
		if strings.TrimSpace(why) == "" {
			return nil, bad("empty why")
		}
		e.Why = why
		m.Entries = append(m.Entries, e)
	}
	if m.Rule == "" {
		return nil, errors.New("manifest has no rule line")
	}
	return m, nil
}

// fenceTexts returns the diagram text of every ipmt fence of a file (the whole
// text for an .ipmt file).
func fenceTexts(path string, data []byte) []string {
	if isIPMT(path) {
		return []string{markdown.NormalizeLF(string(data))}
	}
	_, blocks := markdown.ScanIPMTBlocks(string(data))
	out := make([]string, len(blocks))
	for i, b := range blocks {
		out[i] = b.Content
	}
	return out
}

func hasLine(text, want string) bool {
	for _, l := range strings.Split(text, "\n") {
		if strings.TrimRight(l, "\r") == want {
			return true
		}
	}
	return false
}

func countMarks(s string) (e, t int) {
	return len(reMarkE.FindAllStringIndex(s, -1)), len(reMarkT.FindAllStringIndex(s, -1))
}

// CheckManifest returns one "FAIL path: reason" per violated entry.
func CheckManifest(m *Manifest, read func(rel string) ([]byte, error)) []string {
	var fails []string
	failf := func(path, format string, a ...any) {
		fails = append(fails, fmt.Sprintf("FAIL %s: %s", path, fmt.Sprintf(format, a...)))
	}
	for _, en := range m.Entries {
		data, err := read(en.Path)
		if err != nil {
			failf(en.Path, "cannot read file (%s entry, manifest line %d): %v", en.Kind, en.Src, err)
			continue
		}
		switch en.Kind {
		case KindFile:
			e, t := countMarks(string(data))
			if e < en.E {
				failf(en.Path, "explicit ::e count %d is below the manifest's %d", e, en.E)
			}
			if t < en.T {
				failf(en.Path, "explicit ::t count %d is below the manifest's %d", t, en.T)
			}
		case KindFence:
			var hit []string
			for _, f := range fenceTexts(en.Path, data) {
				if hasLine(f, en.Anchor) {
					hit = append(hit, f)
				}
			}
			if len(hit) != 1 {
				failf(en.Path, "anchor %q matches %d fences, want exactly 1 (manifest line %d)", en.Anchor, len(hit), en.Src)
				continue
			}
			e, t := countMarks(hit[0])
			if e < en.E {
				failf(en.Path, "fence %q: explicit ::e count %d is below the manifest's %d", en.Anchor, e, en.E)
			}
			if t < en.T {
				failf(en.Path, "fence %q: explicit ::t count %d is below the manifest's %d", en.Anchor, t, en.T)
			}
		case KindLine:
			if !hasLine(string(data), en.Text) {
				failf(en.Path, "line no longer present verbatim: %q (manifest line %d)", en.Text, en.Src)
			}
		}
	}
	return fails
}

// CheckManifestRoot loads the manifest file and checks it against the files under root.
// err is only for unreadable input (exit 2); a missing or malformed manifest is a FAIL.
func CheckManifestRoot(root, manifestPath string) (fails []string, err error) {
	data, err := os.ReadFile(manifestPath)
	if errors.Is(err, fs.ErrNotExist) {
		return []string{fmt.Sprintf("FAIL %s: manifest file is missing", filepath.Base(manifestPath))}, nil
	}
	if err != nil {
		return nil, err
	}
	m, perr := ParseManifest(data)
	if perr != nil {
		return []string{fmt.Sprintf("FAIL %s: %v", filepath.Base(manifestPath), perr)}, nil
	}
	return CheckManifest(m, func(rel string) ([]byte, error) {
		return os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	}), nil
}
