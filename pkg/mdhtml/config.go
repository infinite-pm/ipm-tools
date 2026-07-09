package mdhtml

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Config drives the md-html exporter. Loaded from a JSON file with the
// shape documented below. JSON (vs the TOML
// the analysis sketched) keeps v1 stdlib-only — easy to migrate later.
type Config struct {
	// OutDir is the output root. File-level `out` paths are resolved
	// against it.
	OutDir string `json:"out_dir"`

	// ExtraCSS is an optional global stylesheet — linked into every page
	// in addition to the bundled palette.
	ExtraCSS string `json:"extra_css,omitempty"`

	// TitleTemplate, if set, replaces `{{title}}` with the page's first
	// H1 (or basename if there is no H1) and `{{path}}` with the
	// source-relative path. Default: just the title.
	TitleTemplate string `json:"title_template,omitempty"`

	// Header is the top site bar (brand → main site + menu).
	Header HeaderConfig `json:"header"`

	// Footer is the bottom bar (tagline + per-page "view source").
	Footer FooterConfig `json:"footer"`

	// NoTOC disables the auto-generated table of contents. Default is
	// false → TOC is rendered after the H1 of every page that has at
	// least one H2.
	NoTOC bool `json:"no_toc,omitempty"`

	// Files lists explicit src → out mappings. Source paths are
	// resolved relative to the config file's directory.
	Files []FileMapping `json:"file,omitempty"`

	// Globs lists glob patterns; output paths mirror the source tree
	// under OutDir with .md → .html.
	Globs []GlobMapping `json:"glob,omitempty"`
}

// HeaderConfig is the top site bar: a brand that links to the main site, plus a menu.
type HeaderConfig struct {
	HomeURL string `json:"home_url"` // the brand links here (default https://infinite.pm/)
	Brand   string `json:"brand"`    // brand text, dot-split into coloured spans (default "infinite.pm")
	// Logo, when set, replaces that text with the real logo — which is the only
	// thing that gets the name's colours right. Splitting on dots can only give
	// the last SEGMENT one colour, so "pm" comes out all blue where the logo has
	// an orange p and a blue m. Given relative to the output root, and adjusted
	// per page depth, so pages nested in subdirectories still find it.
	Logo    string     `json:"logo"`
	LogoAlt string     `json:"logo_alt"`       // defaults to the brand text
	Menu    []MenuLink `json:"menu,omitempty"` // nav links on the right
}

// MenuLink is one header nav entry.
type MenuLink struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

// FooterConfig is the bottom bar: a tagline plus a per-page "view source" link
// (to the page's own markdown source on GitHub).
type FooterConfig struct {
	Text        string `json:"text,omitempty"`         // tagline / copyright (left)
	Repo        string `json:"repo,omitempty"`         // blank → no source link
	Branch      string `json:"branch,omitempty"`       // default "main"
	SourceLabel string `json:"source_label,omitempty"` // default "GitHub"
}

type FileMapping struct {
	Src string `json:"src"`
	Out string `json:"out"`
}

type GlobMapping struct {
	Src string `json:"src"`
}

// LoadConfig reads + parses the config at the given path and applies
// defaults (Branch, SourceLabel, OutDir).
func LoadConfig(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	if cfg.OutDir == "" {
		cfg.OutDir = "_site"
	}
	if cfg.Header.HomeURL == "" {
		cfg.Header.HomeURL = "https://infinite.pm/"
	}
	if cfg.Header.Brand == "" {
		cfg.Header.Brand = "infinite.pm"
	}
	if cfg.Footer.Branch == "" {
		cfg.Footer.Branch = "main"
	}
	if cfg.Footer.SourceLabel == "" {
		cfg.Footer.SourceLabel = "GitHub"
	}
	return &cfg, nil
}

// ResolvedMapping is one (source .md, output .html) pair after globs
// have been expanded. SrcRel is the source path relative to the
// config root (= directory containing the config file) — used for the
// "View source on GitHub" link.
type ResolvedMapping struct {
	SrcAbs string
	OutAbs string
	SrcRel string
}

// ResolveMappings expands Files + Globs into concrete file pairs.
// `configDir` is the directory containing the config (used as the
// root for relative source paths). Returns mappings sorted by SrcRel
// for deterministic output ordering.
func ResolveMappings(cfg *Config, configDir string) ([]ResolvedMapping, error) {
	outDirAbs := cfg.OutDir
	if !filepath.IsAbs(outDirAbs) {
		outDirAbs = filepath.Join(configDir, outDirAbs)
	}
	seen := map[string]bool{}
	var out []ResolvedMapping

	for _, f := range cfg.Files {
		srcAbs := f.Src
		if !filepath.IsAbs(srcAbs) {
			srcAbs = filepath.Join(configDir, f.Src)
		}
		outAbs := filepath.Join(outDirAbs, f.Out)
		if seen[srcAbs] {
			continue
		}
		seen[srcAbs] = true
		// Derive SrcRel the same way the Globs branch does, so an absolute
		// `src` in the config still yields a config-relative path for the
		// "View source on GitHub" link (a raw absolute f.Src would produce
		// `.../blob/main//home/...`).
		srcRel := f.Src
		if rel, err := filepath.Rel(configDir, srcAbs); err == nil {
			srcRel = rel
		}
		out = append(out, ResolvedMapping{
			SrcAbs: srcAbs, OutAbs: outAbs, SrcRel: srcRel,
		})
	}

	for _, g := range cfg.Globs {
		// filepath.Glob doesn't support **; fall back to a walk.
		matches, err := globMd(configDir, g.Src)
		if err != nil {
			return nil, fmt.Errorf("glob %q: %w", g.Src, err)
		}
		for _, m := range matches {
			if seen[m] {
				continue
			}
			seen[m] = true
			rel, err := filepath.Rel(configDir, m)
			if err != nil {
				return nil, err
			}
			outRel := rel
			if filepath.Ext(outRel) == ".md" {
				outRel = outRel[:len(outRel)-3] + ".html"
			}
			outAbs := filepath.Join(outDirAbs, outRel)
			out = append(out, ResolvedMapping{
				SrcAbs: m, OutAbs: outAbs, SrcRel: rel,
			})
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].SrcRel < out[j].SrcRel })
	return out, nil
}

// globMd walks under configDir and returns all .md files matching the
// pattern. Supports `**/*.md` (recursive) and plain `dir/*.md`.
//
// For `prefix/**/suffix` the walk is rooted at `configDir/prefix`, not
// at `configDir` — so `docs/**/*.md` matches ONLY files under `docs/`,
// not every .md file in the repo.
func globMd(configDir, pattern string) ([]string, error) {
	doubleStarAt := indexOfDoubleStar(pattern)
	if doubleStarAt < 0 {
		// Plain glob — use filepath.Glob directly, anchored at configDir.
		abs := pattern
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(configDir, pattern)
		}
		return filepath.Glob(abs)
	}

	// Split pattern at the FIRST `**/`. Everything before is the literal
	// directory prefix to walk into; everything after is the per-file
	// suffix pattern (matched against the file's basename).
	prefix := pattern[:doubleStarAt]
	rest := pattern[doubleStarAt+2:]
	rest = strings.TrimPrefix(rest, "/")
	if rest == "" {
		rest = "*"
	}

	root := configDir
	if prefix != "" {
		if filepath.IsAbs(prefix) {
			// Absolute pattern (e.g. "/abs/docs/**/*.md"): root the walk at
			// the absolute prefix directly. filepath.Join(configDir, prefix)
			// would drop the leading slash and re-anchor under configDir.
			root = prefix
		} else {
			root = filepath.Join(configDir, prefix)
		}
	}

	var out []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			base := d.Name()
			if base == "node_modules" || base == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		match, _ := filepath.Match(rest, filepath.Base(path))
		if match {
			out = append(out, path)
		}
		return nil
	})
	return out, err
}

// indexOfDoubleStar returns the index of the first `**` in p, or -1.
func indexOfDoubleStar(p string) int {
	for i := 0; i < len(p)-1; i++ {
		if p[i] == '*' && p[i+1] == '*' {
			return i
		}
	}
	return -1
}
