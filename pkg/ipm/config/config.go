// Package config reads the per-repo `.ipm.conf` file shared between
// cmd/md-embed, cmd/ipm-rpc, and any other infinite.pm Go toolchain entry
// point.
//
// File format is a flat `key = value` INI dialect (no sections in v1). `#`
// starts a comment to end-of-line. Whitespace around the `=` and trailing
// spaces are ignored. Unknown keys are an error — they trip pre-commit /
// CI checks. To extend the schema, add a field to Config + a case in the
// parser; tests live next door.
//
// Discovery walks upward from a starting directory looking for `.ipm.conf`
// or a `.git` marker; first match wins. This is the same shape as
// git-config's "repo root" discovery, so it composes cleanly with
// multi-root VS Code workspaces (one Config per workspace folder).
//
// See the design rationale behind moving this out of pkg/mdembed.
package config

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// FileName is the on-disk name of the per-repo config file.
const FileName = ".ipm.conf"

// Config is the parsed contents of a .ipm.conf file. Only fields the file
// actually set are non-nil — that lets a caller distinguish "the file said
// `svg-dir = _ipm`" from "the file didn't mention svg-dir at all" when
// merging with CLI flag defaults.
type Config struct {
	SVGDir *string

	// EmbedIgnore is the list of glob patterns for `.md` files md-embed must
	// skip entirely — never scanned, rendered, or rewritten. Use it for docs
	// that contain illustrative or intentionally-invalid ipmt the tool would
	// otherwise try to process (e.g. the md-embed tutorial, an "invalid
	// examples" gallery).
	//
	// Set via one or more `embed-ignore = …` lines; each line is a comma- or
	// whitespace-separated list of patterns and the lines accumulate. Patterns
	// are matched against each file's path relative to the config's directory
	// (forward slashes). Beyond filepath.Match's *, ?, [], a ** spans any
	// number of path segments — so `docs/**/draft-*.md` matches at any depth
	// under docs/, while `docs/*.md` matches only direct children.
	EmbedIgnore []string

	// Future fields go here. Keep the surface small; behavior toggles
	// (verbose, prune, check) stay CLI-only so non-interactive runs don't
	// surprise users with repo-level defaults.
}

// IsIgnored reports whether rel — a path relative to the config's directory,
// either OS- or slash-separated — matches any EmbedIgnore pattern.
func (c Config) IsIgnored(rel string) bool {
	rel = filepath.ToSlash(rel)
	for _, pat := range c.EmbedIgnore {
		if matchGlob(filepath.ToSlash(pat), rel) {
			return true
		}
	}
	return false
}

// matchGlob reports whether the slash-separated path rel matches the glob
// pattern. It extends path.Match (which never matches across "/") with `**`,
// a segment that matches zero or more path segments.
func matchGlob(pattern, rel string) bool {
	return globSegMatch(strings.Split(pattern, "/"), strings.Split(rel, "/"))
}

// validateGlob reports whether pattern is a well-formed matchGlob pattern. It
// mirrors how globSegMatch consumes the pattern: each "/"-separated segment is
// fed to path.Match (except "**", which globSegMatch handles specially), so a
// segment that path.Match rejects with ErrBadPattern (e.g. an unclosed "[")
// would otherwise silently match nothing. Returns path.ErrBadPattern for the
// first malformed segment, nil when every segment is valid.
func validateGlob(pattern string) error {
	for _, seg := range strings.Split(filepath.ToSlash(pattern), "/") {
		if seg == "**" {
			continue
		}
		if _, err := path.Match(seg, ""); err != nil {
			return err
		}
	}
	return nil
}

// globSegMatch matches the remaining pattern segments against the remaining
// path segments. A "**" segment consumes any number (including zero) of path
// segments; every other segment is matched 1:1 with path.Match.
func globSegMatch(pat, name []string) bool {
	for len(pat) > 0 {
		if pat[0] == "**" {
			rest := pat[1:]
			if len(rest) == 0 {
				return true // trailing ** soaks up whatever is left
			}
			for i := 0; i <= len(name); i++ {
				if globSegMatch(rest, name[i:]) {
					return true
				}
			}
			return false
		}
		if len(name) == 0 {
			return false
		}
		if ok, _ := path.Match(pat[0], name[0]); !ok {
			return false
		}
		pat, name = pat[1:], name[1:]
	}
	return len(name) == 0
}

// FindRepoRoot walks upward from start, returning the first ancestor
// directory that contains either a `.git` entry or a .ipm.conf file. If
// neither is found before reaching the filesystem root, it returns start
// unchanged.
//
// `.git` is recognized as either a directory (regular checkout) or a file
// (git worktree / submodule).
func FindRepoRoot(start string) string {
	absStart, err := filepath.Abs(start)
	if err != nil {
		return start
	}
	abs := absStart
	for {
		if _, err := os.Stat(filepath.Join(abs, FileName)); err == nil {
			return abs
		}
		if _, err := os.Stat(filepath.Join(abs, ".git")); err == nil {
			return abs
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			break
		}
		abs = parent
	}
	return absStart
}

// Load reads `<rootDir>/.ipm.conf` if it exists and parses `key = value`
// lines (with `#` comments). Returns a zero Config and ok=false when no
// file is present. Returns an error only on read or parse problems.
func Load(rootDir string) (Config, bool, error) {
	path := filepath.Join(rootDir, FileName)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Config{}, false, nil
		}
		return Config{}, false, fmt.Errorf("read %s: %w", path, err)
	}
	cfg, err := parse(string(data), path)
	if err != nil {
		return Config{}, false, err
	}
	return cfg, true, nil
}

func parse(text, sourceName string) (Config, error) {
	var out Config
	for lineNo, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Strip inline comments — split on the first '#' that isn't inside a
		// quoted value (we don't support quoting yet, so simple split).
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = strings.TrimSpace(line[:i])
			if line == "" {
				continue
			}
		}
		eq := strings.IndexByte(line, '=')
		if eq <= 0 {
			return Config{}, fmt.Errorf("%s:%d: expected key=value, got %q", sourceName, lineNo+1, raw)
		}
		key := strings.TrimSpace(line[:eq])
		val := strings.TrimSpace(line[eq+1:])
		switch key {
		case "svg-dir":
			v := val
			out.SVGDir = &v
		case "embed-ignore":
			// Comma- or whitespace-separated; repeated lines accumulate.
			for _, p := range strings.FieldsFunc(val, func(r rune) bool {
				return r == ',' || r == ' ' || r == '\t'
			}) {
				// Reject a malformed glob (e.g. an unclosed `[`) loudly rather
				// than letting it silently match nothing — matching the
				// hard-error treatment of unknown keys above.
				if err := validateGlob(p); err != nil {
					return Config{}, fmt.Errorf("%s:%d: bad embed-ignore pattern %q: %w", sourceName, lineNo+1, p, err)
				}
				out.EmbedIgnore = append(out.EmbedIgnore, p)
			}
		default:
			return Config{}, fmt.Errorf("%s:%d: unknown key %q", sourceName, lineNo+1, key)
		}
	}
	return out, nil
}
