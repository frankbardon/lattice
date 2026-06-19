package schema

import (
	"encoding/json"
	"net/url"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/spf13/afero"

	"github.com/frankbardon/lattice/errors"
)

// URLFetcher fetches a schema document for an absolute http(s) $ref that is not
// served by the local catalog. It exists so the URL code path is real and
// testable offline: production wiring may supply a network-backed fetcher,
// tests supply a fixture-backed one. A nil fetcher means "URLs must resolve via
// the catalog"; an unknown URL then fails fast as SCHEMA_REF_UNRESOLVED.
type URLFetcher func(u *url.URL) (*jsonschema.Schema, error)

// Loader reads dashboard documents over an afero filesystem and resolves every
// item-type $ref against a Catalog, producing a ResolvedGraph for E1-S4.
type Loader struct {
	fs       afero.Fs
	catalog  *Catalog
	fetcher  URLFetcher
	dashSch  *jsonschema.Schema
	relRoots []string
}

// LoaderOption configures a Loader.
type LoaderOption func(*Loader)

// WithURLFetcher sets the fetcher used for absolute http(s) refs that the
// catalog does not already serve.
func WithURLFetcher(f URLFetcher) LoaderOption {
	return func(l *Loader) { l.fetcher = f }
}

// WithRelativeRoots sets additional filesystem directories against which
// relative $refs are resolved (in addition to catalog-id normalization). Refs
// like "table.schema.json" or "items/table.schema.json" are looked up as files
// under these roots.
func WithRelativeRoots(dirs ...string) LoaderOption {
	return func(l *Loader) { l.relRoots = append(l.relRoots, dirs...) }
}

// NewLoader constructs a Loader. dashboardSchema is the parsed top-level
// dashboard schema (dashboard.schema.json), carried into the graph for E1-S4's
// structural pass; it may be nil if unavailable.
func NewLoader(fs afero.Fs, catalog *Catalog, dashboardSchema *jsonschema.Schema, opts ...LoaderOption) *Loader {
	l := &Loader{
		fs:      fs,
		catalog: catalog,
		dashSch: dashboardSchema,
	}
	for _, o := range opts {
		o(l)
	}
	return l
}

// Load reads the dashboard document at docPath, decodes its instance tree, and
// resolves every $ref, returning the linked ResolvedGraph. The first
// unresolvable ref or version mismatch fails fast with a CodedError.
func (l *Loader) Load(docPath string) (*ResolvedGraph, error) {
	data, err := afero.ReadFile(l.fs, docPath)
	if err != nil {
		return nil, errors.WrapCodedError(err, errors.SCHEMA_IO, "failed reading dashboard document "+docPath)
	}
	return l.parse(data, docPath)
}

// LoadBytes decodes a dashboard document from raw bytes (rather than reading it
// from the filesystem) and resolves every $ref, returning the linked
// ResolvedGraph. source is used only for diagnostics. This is the byte-oriented
// entry point used by the resolver, which has already read the document for its
// structural pass and wants to avoid a second read.
func (l *Loader) LoadBytes(data []byte, source string) (*ResolvedGraph, error) {
	return l.parse(data, source)
}

// parse decodes raw document bytes and resolves the tree. Split out for tests
// that supply bytes directly.
func (l *Loader) parse(data []byte, source string) (*ResolvedGraph, error) {
	var doc Document
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, errors.WrapCodedError(err, errors.SCHEMA_INVALID, "failed decoding dashboard document "+source)
	}
	if doc.Root == nil {
		return nil, errors.NewCodedErrorWithDetails(errors.SCHEMA_INVALID,
			"dashboard document has no root instance", map[string]any{"source": source})
	}

	g := &ResolvedGraph{
		Document:        &doc,
		DashboardSchema: l.dashSch,
		Types:           make(map[string]*ResolvedType),
		Refs:            make(map[*Instance]string),
	}
	if err := l.resolveInstance(g, doc.Root); err != nil {
		return nil, err
	}
	return g, nil
}

// resolveInstance resolves one node's $ref then recurses into children.
func (l *Loader) resolveInstance(g *ResolvedGraph, inst *Instance) error {
	rt, err := l.resolveRef(g, inst.Ref)
	if err != nil {
		return err
	}
	g.Types[rt.ID] = rt
	g.Refs[inst] = rt.ID

	for _, child := range inst.Children {
		if err := l.resolveInstance(g, child); err != nil {
			return err
		}
	}
	return nil
}

// resolveRef dispatches a raw $ref across the three supported forms:
//   - inline fragment ("#/$defs/...")          -> resolved against the document
//   - absolute URL ("https://.../items/...")   -> catalog ($id) then URLFetcher
//   - relative path ("items/table/1.0.0", ...) -> catalog-id normalization then
//     relative-root files
//
// Version pinning is enforced for catalog-keyed forms: a known name with an
// unknown version fails as SCHEMA_VERSION_MISMATCH; an entirely unknown ref
// fails as SCHEMA_REF_UNRESOLVED.
func (l *Loader) resolveRef(g *ResolvedGraph, ref string) (*ResolvedType, error) {
	if ref == "" {
		return nil, errors.NewCodedError(errors.SCHEMA_REF_UNRESOLVED, "instance is missing a $ref")
	}

	if strings.HasPrefix(ref, "#") {
		return l.resolveInline(g, ref)
	}

	if u, err := url.Parse(ref); err == nil && u.IsAbs() {
		return l.resolveAbsolute(ref, u)
	}

	return l.resolveRelative(ref)
}

// resolveInline resolves a "#/$defs/<name>" fragment against the document's
// inline $defs.
func (l *Loader) resolveInline(g *ResolvedGraph, ref string) (*ResolvedType, error) {
	const prefix = "#/$defs/"
	if !strings.HasPrefix(ref, prefix) {
		return nil, errors.NewCodedErrorWithDetails(errors.SCHEMA_REF_UNRESOLVED,
			"unsupported inline $ref fragment", map[string]any{"ref": ref})
	}
	name := strings.TrimPrefix(ref, prefix)
	sch, ok := g.Document.Defs[name]
	if !ok || sch == nil {
		return nil, errors.NewCodedErrorWithDetails(errors.SCHEMA_REF_UNRESOLVED,
			"inline $ref does not resolve to a document $defs entry",
			map[string]any{"ref": ref})
	}
	return &ResolvedType{
		ID:      ref,
		Name:    nameOf(sch.ID),
		Version: versionOf(sch.ID),
		Schema:  sch,
		Source:  "inline:" + ref,
	}, nil
}

// resolveAbsolute resolves an absolute http(s) $ref. The catalog is consulted
// first (the base URL is an identifier namespace, not a fetch target). If the
// exact $id is absent but the type name is catalogued, that is a version
// mismatch. Otherwise an optional URLFetcher is tried; failing that, the ref is
// unresolved.
func (l *Loader) resolveAbsolute(ref string, u *url.URL) (*ResolvedType, error) {
	if rt, ok := l.catalog.lookupID(ref); ok {
		return rt, nil
	}
	if mismatch := l.versionMismatch(ref); mismatch != nil {
		return nil, mismatch
	}

	if l.fetcher != nil {
		sch, err := l.fetcher(u)
		if err != nil {
			return nil, errors.WrapCodedError(err, errors.SCHEMA_REF_UNRESOLVED,
				"failed fetching schema for $ref "+ref)
		}
		if sch == nil {
			return nil, errors.NewCodedErrorWithDetails(errors.SCHEMA_REF_UNRESOLVED,
				"URL fetcher returned no schema for $ref", map[string]any{"ref": ref})
		}
		return &ResolvedType{
			ID:      idOrRef(sch, ref),
			Name:    nameOf(ref),
			Version: versionOf(ref),
			Schema:  sch,
			Source:  ref,
		}, nil
	}

	return nil, errors.NewCodedErrorWithDetails(errors.SCHEMA_REF_UNRESOLVED,
		"absolute $ref is not in the catalog and no URL fetcher is configured",
		map[string]any{"ref": ref})
}

// resolveRelative resolves a relative $ref. It is first normalized onto the
// catalog base URL and looked up by $id (so "items/table/1.0.0" finds the
// catalog entry). Version pinning applies to that normalized id. If that fails,
// the ref is tried as a file path under each configured relative root.
func (l *Loader) resolveRelative(ref string) (*ResolvedType, error) {
	normalized := normalizeRelativeRef(ref)
	if rt, ok := l.catalog.lookupID(normalized); ok {
		return rt, nil
	}
	if mismatch := l.versionMismatch(normalized); mismatch != nil {
		return nil, mismatch
	}

	for _, root := range l.relRoots {
		p := joinPath(root, ref)
		exists, _ := afero.Exists(l.fs, p)
		if !exists {
			continue
		}
		data, err := afero.ReadFile(l.fs, p)
		if err != nil {
			return nil, errors.WrapCodedError(err, errors.SCHEMA_IO, "failed reading relative schema "+p)
		}
		sch, err := parseSchema(data)
		if err != nil {
			return nil, errors.WrapCodedError(err, errors.SCHEMA_INVALID, "failed parsing relative schema "+p)
		}
		return &ResolvedType{
			ID:      idOrRef(sch, normalized),
			Name:    nameOf(idOrRef(sch, normalized)),
			Version: versionOf(idOrRef(sch, normalized)),
			Schema:  sch,
			Source:  p,
		}, nil
	}

	return nil, errors.NewCodedErrorWithDetails(errors.SCHEMA_REF_UNRESOLVED,
		"relative $ref did not resolve via the catalog or any relative root",
		map[string]any{"ref": ref, "normalized": normalized})
}

// versionMismatch returns a SCHEMA_VERSION_MISMATCH CodedError when the
// canonical id names a known item type but the pinned version is not
// catalogued. It returns nil when the name is unknown (caller falls through to
// other resolution paths / SCHEMA_REF_UNRESOLVED).
func (l *Loader) versionMismatch(canonicalID string) error {
	name := nameOf(canonicalID)
	version := versionOf(canonicalID)
	if name == "" || !l.catalog.hasName(name) {
		return nil
	}
	// Name is catalogued but this exact id was not found above => version is
	// missing or mismatched.
	return errors.NewCodedErrorWithDetails(errors.SCHEMA_VERSION_MISMATCH,
		"referenced item type version is not available in the catalog",
		map[string]any{
			"ref":       canonicalID,
			"name":      name,
			"requested": version,
			"available": l.catalog.availableVersions(name),
		})
}

// idOrRef returns the schema's own $id if present, else the supplied fallback.
func idOrRef(sch *jsonschema.Schema, fallback string) string {
	if sch != nil && sch.ID != "" {
		return sch.ID
	}
	return fallback
}

// joinPath joins a root and a relative ref without importing path twice across
// files; trailing/leading slashes are normalized.
func joinPath(root, ref string) string {
	root = strings.TrimRight(root, "/")
	ref = strings.TrimLeft(ref, "/")
	if root == "" {
		return ref
	}
	return root + "/" + ref
}
