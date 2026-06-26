// Package skills is lattice's embedded skill corpus: a self-contained bundle of
// markdown guides + references (each carrying YAML frontmatter) that the MCP
// layer serves to model hosts as workflow/usage documentation.
//
// Boundary rule (non-negotiable): this package is PURE EMBEDDED DATA. It imports
// nothing from internal/* (only the standard library), so the MCP layer — itself
// restricted to the public ./service facade plus the module-root errors package —
// can consume it without breaching the import boundary.
//
// Each skill lives in a sibling `*.md` file embedded at build time. A file's
// frontmatter (the leading `---`-delimited block) declares its Metadata; the
// markdown body after the block is the skill content served verbatim. The
// frontmatter schema is documented in session-bootstrap.md (the keystone skill)
// and parsed by parseMetadata below.
package skills

import (
	"embed"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
)

//go:embed *.md
var content embed.FS

// Metadata is the typed view of a skill file's YAML frontmatter. It is the
// catalog entry List returns and the manifest/list_skills tools serialize; the
// json tags match the frontmatter keys so a Metadata round-trips to the same
// schema authors write.
type Metadata struct {
	// Name is the skill's stable identifier — the get_skill argument and the
	// file stem (foo.md → "foo"). Defaults to the file stem when frontmatter
	// omits the key.
	Name string `json:"name"`
	// Description is a one-line summary used in list_skills / get_manifest so a
	// host can pick the right skill without fetching its body.
	Description string `json:"description"`
	// Type is the skill's register: "guide" (a how-to / workflow narrative) or
	// "reference" (a lookup table / catalog the host consults on demand).
	Type string `json:"type"`
	// Kind classifies the skill's shape: "workflow" (an ordered procedure),
	// "items" (a per-item catalog), or "tool" (focused on one MCP tool).
	Kind string `json:"kind,omitempty"`
	// AppliesTo lists the MCP tool names (or flows) this skill is relevant to,
	// so a host can surface the skill alongside those tools.
	AppliesTo []string `json:"applies_to"`
	// Covers optionally lists the item types / tools a reference skill enumerates
	// (e.g. the item-type names a catalog reference documents). Empty when the
	// skill is not a catalog.
	Covers []string `json:"covers,omitempty"`
}

// List walks the embedded corpus for *.md files, parses each file's frontmatter,
// and returns the resulting Metadata slice sorted by Name (a stable, deterministic
// ordering across builds).
//
// A file with no frontmatter block is skipped silently rather than crashing the
// loader: a frontmatter-less file in this package is a code-review smell, but the
// loader is the wrong place to panic on it. Non-`.md` entries and directories are
// ignored.
func List() []Metadata {
	entries, err := fs.ReadDir(content, ".")
	if err != nil {
		// content is a //go:embed FS rooted at the package directory; ReadDir
		// against "." cannot meaningfully fail at runtime. Treat any error as a
		// corrupt embed (a build/programming error) and surface it loudly.
		panic("skills: cannot read embedded content: " + err.Error())
	}
	out := make([]Metadata, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if filepath.Ext(name) != ".md" {
			continue
		}
		data, err := fs.ReadFile(content, name)
		if err != nil {
			continue
		}
		md, ok := parseMetadata(string(data))
		if !ok {
			continue
		}
		// Default Name to the file stem when frontmatter omits it, keeping the
		// loader robust against a missing key for additive future skills.
		if md.Name == "" {
			md.Name = strings.TrimSuffix(name, ".md")
		}
		out = append(out, md)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Get returns the raw markdown body for the named skill (the file content
// including its frontmatter block, served verbatim). The name must NOT include
// the .md extension. It returns the content and true on a hit, or "" and false
// when no such skill exists.
func Get(name string) (string, bool) {
	data, err := fs.ReadFile(content, name+".md")
	if err != nil {
		return "", false
	}
	return string(data), true
}

// parseFrontmatter extracts the raw key→value pairs from a markdown file's
// leading `---`-delimited YAML frontmatter block. List-valued keys (applies_to,
// covers) are captured as the raw post-colon string — parseList turns them into
// a slice. A file without a well-formed frontmatter block yields an empty map.
func parseFrontmatter(md string) map[string]string {
	result := make(map[string]string)
	if !strings.HasPrefix(md, "---\n") {
		return result
	}
	end := strings.Index(md[4:], "\n---")
	if end < 0 {
		return result
	}
	block := md[4 : 4+end]
	for _, line := range strings.Split(block, "\n") {
		parts := strings.SplitN(line, ":", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			val := strings.TrimSpace(parts[1])
			result[key] = val
		}
	}
	return result
}

// parseMetadata builds the typed Metadata from a markdown file's frontmatter.
// It returns (zero, false) when the file carries no frontmatter block, so List
// can skip it.
func parseMetadata(md string) (Metadata, bool) {
	fm := parseFrontmatter(md)
	if len(fm) == 0 {
		return Metadata{}, false
	}
	return Metadata{
		Name:        fm["name"],
		Description: fm["description"],
		Type:        fm["type"],
		Kind:        fm["kind"],
		AppliesTo:   parseList(fm["applies_to"]),
		Covers:      parseList(fm["covers"]),
	}, true
}

// parseList parses a YAML list value that parseFrontmatter captured as a single
// trimmed string. Both supported authoring forms reduce to the same slice:
//
//   - bracket inline: `["a", "b", "c"]`
//   - comma-separated CSV: `a, b, c`
//
// Empty input returns nil. Quoted entries have surrounding single/double quotes
// stripped, and empty fragments (e.g. a trailing comma) are dropped.
func parseList(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if strings.HasPrefix(raw, "[") && strings.HasSuffix(raw, "]") {
		raw = raw[1 : len(raw)-1]
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		p = strings.Trim(p, `"'`)
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
