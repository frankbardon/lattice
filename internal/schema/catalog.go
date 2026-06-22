package schema

import (
	"os"
	"path"
	"regexp"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/spf13/afero"

	"github.com/frankbardon/lattice/errors"
)

// catalogBaseURL is the identifier namespace for catalog schema $ids. It is NOT
// a live fetch target; schemas are keyed by $id from the local catalog (per the
// E1-S2 followup).
const catalogBaseURL = "https://lattice.dev/schemas/"

// versionedIDRe extracts the name and semver version from the tail of a
// versioned $id path, e.g. ".../items/table/1.0.0" -> name "table", version
// "1.0.0". The name is the path segment immediately preceding the version.
var versionedIDRe = regexp.MustCompile(`([^/]+)/([0-9]+\.[0-9]+\.[0-9]+)$`)

// Catalog is an in-memory index of item-type schemas keyed by canonical $id.
// It is populated from one or more directories on an afero filesystem.
type Catalog struct {
	fs afero.Fs

	// byID maps a canonical $id to its parsed schema entry.
	byID map[string]*ResolvedType

	// byName maps an item-type name to the set of versioned entries available,
	// keyed by version. Used to distinguish "name unknown" from
	// "version mismatch".
	byName map[string]map[string]*ResolvedType
}

// NewCatalog builds a Catalog by reading every *.schema.json file under the
// given directories on fs (recursively). Each file must declare an $id; entries
// are keyed by that $id. A schema that fails to parse yields a SCHEMA_INVALID
// CodedError; a schema missing an $id yields SCHEMA_INVALID.
func NewCatalog(fs afero.Fs, dirs ...string) (*Catalog, error) {
	c := &Catalog{
		fs:     fs,
		byID:   make(map[string]*ResolvedType),
		byName: make(map[string]map[string]*ResolvedType),
	}
	for _, dir := range dirs {
		if err := c.loadDir(dir); err != nil {
			return nil, err
		}
	}
	return c, nil
}

func (c *Catalog) loadDir(dir string) error {
	return afero.Walk(c.fs, dir, func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			return errors.WrapCodedError(err, errors.SCHEMA_IO, "failed walking catalog directory "+dir)
		}
		if fi.IsDir() || !strings.HasSuffix(p, ".schema.json") {
			return nil
		}
		return c.loadFile(p)
	})
}

// loadFile parses a single schema file and indexes it by its $id.
func (c *Catalog) loadFile(p string) error {
	data, err := afero.ReadFile(c.fs, p)
	if err != nil {
		return errors.WrapCodedError(err, errors.SCHEMA_IO, "failed reading schema file "+p)
	}
	sch, err := parseSchema(data)
	if err != nil {
		return errors.WrapCodedError(err, errors.SCHEMA_INVALID, "failed parsing schema file "+p)
	}
	if sch.ID == "" {
		return errors.NewCodedErrorWithDetails(errors.SCHEMA_INVALID,
			"schema file is missing required $id", map[string]any{"file": p})
	}
	c.index(&ResolvedType{
		ID:      sch.ID,
		Name:    nameOf(sch.ID),
		Version: versionOf(sch.ID),
		Schema:  sch,
		Source:  p,
	})
	return nil
}

func (c *Catalog) index(rt *ResolvedType) {
	c.byID[rt.ID] = rt
	if rt.Name != "" && rt.Version != "" {
		versions := c.byName[rt.Name]
		if versions == nil {
			versions = make(map[string]*ResolvedType)
			c.byName[rt.Name] = versions
		}
		versions[rt.Version] = rt
	}
}

// lookupID returns the catalog entry for an exact canonical $id.
func (c *Catalog) lookupID(id string) (*ResolvedType, bool) {
	rt, ok := c.byID[id]
	return rt, ok
}

// Lookup returns the catalogued schema for an exact canonical $id, or nil if no
// entry is indexed under that id. It is the exported view of the by-$id index,
// used by the resolver to serve non-item shared schemas (e.g. the theme
// vocabulary) to the JSON-Schema compiler when a document/item schema $refs them.
func (c *Catalog) Lookup(id string) *ResolvedType {
	if rt, ok := c.byID[id]; ok {
		return rt
	}
	return nil
}

// positionalKey is the reserved item-type schema keyword that designates a type
// as a layout-only POSITIONAL REGION (E3-S1): a node that only positions
// children and carries no chrome/theme of its own. Like `configurable` and
// `expectedResult` it is a top-level, schema-level keyword (a sibling of
// `properties`, not per-instance config), captured by google/jsonschema-go as an
// unknown keyword in Schema.Extra. The grammar pass (E3-S2) reads this marker —
// it is the SINGLE SOURCE OF TRUTH for which types are legal positional regions;
// no type list is hardcoded anywhere.
const positionalKey = "positional"

// IsPositional reports whether the resolved item-type schema is a positional
// region. It is the exported accessor the grammar pass (E3-S2) consumes to
// decide, without any hardcoded type list, whether a type may appear as a root
// or container child.
//
// Positional-ness has TWO equivalent spellings, both honored here so the fold-in
// of `positional` into the `latticeBehavior` vocabulary (see behavior.go) does
// not break either authoring style downstream this story:
//   - the legacy schema-level `positional: true` keyword, and
//   - the generalized `latticeBehavior.role == "region"`.
//
// Callers migrate to reading Role() directly in E3; until then IsPositional()
// remains the stable bridge. A type that declares neither, or carries no schema,
// reports false.
func (rt *ResolvedType) IsPositional() bool {
	if rt == nil || rt.Schema == nil || rt.Schema.Extra == nil {
		return false
	}
	if rt.Role() == RoleRegion {
		return true
	}
	v, ok := rt.Schema.Extra[positionalKey].(bool)
	return ok && v
}

// IsPositional reports whether the item type indexed under the given canonical
// $id is marked as a positional region. Unknown ids report false.
func (c *Catalog) IsPositional(id string) bool {
	return c.byID[id].IsPositional()
}

// hasName reports whether any version of the named item type is catalogued.
func (c *Catalog) hasName(name string) bool {
	_, ok := c.byName[name]
	return ok
}

// availableVersions returns the catalogued versions for a name (may be empty).
func (c *Catalog) availableVersions(name string) []string {
	versions := c.byName[name]
	out := make([]string, 0, len(versions))
	for v := range versions {
		out = append(out, v)
	}
	return out
}

// parseSchema decodes raw JSON into a jsonschema.Schema, exercising the
// google/jsonschema-go parse path. errors are returned bare for the caller to
// wrap with file context.
func parseSchema(data []byte) (*jsonschema.Schema, error) {
	var s jsonschema.Schema
	if err := s.UnmarshalJSON(data); err != nil {
		return nil, err
	}
	return &s, nil
}

// nameOf extracts the item-type name from a versioned $id, or "" if the id is
// not versioned (e.g. the dashboard schema id ".../dashboard/1.0.0" -> name
// "dashboard").
func nameOf(id string) string {
	if m := versionedIDRe.FindStringSubmatch(id); m != nil {
		return m[1]
	}
	return ""
}

// versionOf extracts the pinned semver from a versioned $id, or "".
func versionOf(id string) string {
	if m := versionedIDRe.FindStringSubmatch(id); m != nil {
		return m[2]
	}
	return ""
}

// normalizeRelativeRef converts a relative ref (e.g. "items/table/1.0.0" or
// "./table/1.0.0") into a candidate canonical $id by joining it onto the
// catalog base URL. The cleaned path is appended to catalogBaseURL.
func normalizeRelativeRef(ref string) string {
	cleaned := path.Clean(ref)
	cleaned = strings.TrimPrefix(cleaned, "/")
	return catalogBaseURL + cleaned
}
