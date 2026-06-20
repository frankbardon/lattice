package resolver

// This file implements the E4-S2 DOCUMENT-SCOPE SURFACES: the configurable
// surface declared for each reserved document scope (`$manifest`, `$variables`,
// `$connections`, `$theme`, `$root`) ON THE DOCUMENT SCHEMA, plus the per-scope
// editor-form generation a configurator targeting that scope produces.
//
// A reserved `$`-target (E4-S1) routes a configurator to a document-LEVEL scope
// rather than an item id. E4-S1 left a seam: a recognized reserved target attached
// a present-but-EMPTY generated form. This story fills that seam by giving each
// scope a real configurable SURFACE — the honest, machine-readable list of the
// legal, runtime-tunable fields of that scope — and GENERATING a document-level
// editor form from it, REUSING the existing item form-generation path
// (generateForm in configurator.go). No parallel generator is built.
//
// WHERE THE SURFACES LIVE. The reserved scopes are document-level, not item
// types, so their surfaces are declared on the DOCUMENT schema
// (schemas/dashboard.schema.json) under a top-level `documentScopes` keyword —
// captured by google/jsonschema-go as an unknown keyword in Schema.Extra, exactly
// like the item-type `configurable` keyword. `documentScopes` maps each reserved
// `$`-keyword to a `configurable`-shaped object (field -> descriptor), so the
// descriptor shape and validation are IDENTICAL to an item surface (shared via
// buildSurface in surface.go). A scope with no declared entry (or an empty one)
// yields an EMPTY but PRESENT surface — the configurator still gets a present
// (empty) form, mirroring a surface-less item.
//
// THE GUARDRAIL. Each scope surface DOUBLES as the guardrail: it enumerates the
// legal target fields a JSON Patch (a future story) may touch within that scope.
// Because every declared field is validated against the scope's REAL schema
// properties (the manifest's properties for `$manifest`, the theme vocabulary's
// tokens for `$theme`), the surface can never drift out of sync with what the
// scope actually accepts — the same honesty guarantee the item surfaces give.
//
// THE RESOLVER STAYS DUMB. This is GENERATION ONLY. The resolver applies NO change
// to the document from a scope surface or its generated form; it merely reads the
// declared surface and emits the editor. Patch application lives in a later story.

import (
	"github.com/google/jsonschema-go/jsonschema"

	"github.com/frankbardon/lattice/internal/schema"
)

// documentScopesKey is the reserved DOCUMENT-schema keyword that declares the
// configurable surfaces of the reserved document scopes (E4-S2). Like the
// item-type `configurable` keyword it is a top-level, schema-level keyword
// captured by google/jsonschema-go as an unknown keyword in Schema.Extra. It maps
// each reserved `$`-scope keyword (e.g. "$theme") to a `configurable`-shaped
// object (field -> descriptor).
const documentScopesKey = "documentScopes"

// documentScopeSurfaces resolves the configurable surface for every reserved
// document scope from the document schema's `documentScopes` keyword, validating
// each scope's declaration with the SAME validator the item-type surfaces use
// (buildSurface). It returns a map keyed by the reserved `$`-keyword; a scope the
// document schema declares no surface for is absent from the map (the caller
// treats an absent scope as a present-but-empty surface). It is fail-fast: the
// first malformed scope surface stops resolution and is returned as
// CONFIGURABLE_SURFACE_INVALID.
//
// Field-name legality is checked per scope against that scope's REAL schema
// property set (the guardrail source): `$manifest` against the manifest's
// properties, `$theme` against the theme vocabulary's tokens. A scope with no
// natural property source (`$variables`, `$connections`, `$root`) accepts no
// fields, so a non-empty surface for it is rejected — keeping every surface
// honest about what its scope can actually tune.
func documentScopeSurfaces(dash *jsonschema.Schema, themeTokens map[string]struct{}) (map[string][]ConfigurableField, error) {
	decls := documentScopeDecls(dash)
	if len(decls) == 0 {
		return nil, nil
	}

	out := make(map[string][]ConfigurableField, len(decls))
	for _, keyword := range sortedFields(decls) {
		decl, ok := decls[keyword].(map[string]any)
		if !ok {
			// A scope entry that is not a `configurable`-shaped object is treated as
			// declaring no surface (an empty, present surface). The document schema's
			// own structural validation is the place to reject a malformed shape; here
			// we degrade gracefully rather than invent a second structural check.
			continue
		}
		props := scopeProperties(keyword, dash, themeTokens)
		surface, err := buildSurface(decl, sortedFields(decl), flatProps(props), keyword, scopePath(keyword))
		if err != nil {
			return nil, err
		}
		out[keyword] = surface
	}
	return out, nil
}

// documentScopeDecls returns the verbatim `documentScopes` mapping declared on the
// document schema (reserved `$`-keyword -> `configurable`-shaped object), captured
// in the schema's Extra keywords. Returns nil when the schema declares none.
func documentScopeDecls(dash *jsonschema.Schema) map[string]any {
	if dash == nil || dash.Extra == nil {
		return nil
	}
	raw, ok := dash.Extra[documentScopesKey]
	if !ok {
		return nil
	}
	decl, _ := raw.(map[string]any)
	return decl
}

// scopeProperties returns the legal field names for one reserved document scope —
// the guardrail source the scope surface's field names are validated against.
//
//   - $manifest: the manifest object's declared properties (title, description, …).
//   - $theme:    the theme vocabulary's tokens (emphasis, spacing, …), drawn from
//     the catalogued theme schema (E2-S1), so the theme scope surface can only
//     enumerate real, in-vocabulary tokens.
//   - $variables / $connections / $root: no top-level scalar fields are
//     runtime-tunable yet, so these accept no surface fields (an empty set).
func scopeProperties(keyword string, dash *jsonschema.Schema, themeTokens map[string]struct{}) map[string]struct{} {
	switch keyword {
	case "$manifest":
		return manifestProperties(dash)
	case "$theme":
		return themeTokens
	default:
		return map[string]struct{}{}
	}
}

// manifestProperties returns the property names of the document schema's manifest
// definition (#/$defs/manifest), the legal field set for the `$manifest` scope.
// Returns an empty set when the definition is missing or carries no properties.
func manifestProperties(dash *jsonschema.Schema) map[string]struct{} {
	props := map[string]struct{}{}
	if dash == nil || dash.Defs == nil {
		return props
	}
	manifest := dash.Defs["manifest"]
	if manifest == nil {
		return props
	}
	for name := range manifest.Properties {
		props[name] = struct{}{}
	}
	return props
}

// themeTokenProperties returns the token names of the catalogued theme vocabulary
// (E2-S1), the legal field set for the `$theme` scope. The theme schema composes
// its base tokens via `allOf -> #/$defs/baseTokens`, so the tokens are read from
// that definition (and, defensively, from any properties declared directly on the
// theme schema or its allOf members). Returns an empty set when the theme schema
// is not catalogued.
func themeTokenProperties(cat *schema.Catalog) map[string]struct{} {
	tokens := map[string]struct{}{}
	if cat == nil {
		return tokens
	}
	rt := cat.Lookup(themeSchemaID)
	if rt == nil || rt.Schema == nil {
		return tokens
	}
	collectThemeTokens(rt.Schema, tokens, 0)
	return tokens
}

// collectThemeTokens gathers property names from a theme (sub)schema, following its
// `allOf` members and the `baseTokens` $def. depth bounds the recursion as a
// defensive guard against a pathological schema; the real theme schema nests only
// one level (theme -> allOf -> baseTokens).
func collectThemeTokens(s *jsonschema.Schema, into map[string]struct{}, depth int) {
	if s == nil || depth > 8 {
		return
	}
	for name := range s.Properties {
		into[name] = struct{}{}
	}
	if bt := themeBaseTokens(s); bt != nil {
		for name := range bt.Properties {
			into[name] = struct{}{}
		}
	}
	for _, member := range s.AllOf {
		collectThemeTokens(member, into, depth+1)
	}
}

// themeBaseTokens returns the theme schema's #/$defs/baseTokens definition when
// present, the canonical home of the base theme tokens.
func themeBaseTokens(s *jsonschema.Schema) *jsonschema.Schema {
	if s == nil || s.Defs == nil {
		return nil
	}
	return s.Defs["baseTokens"]
}

// scopePath is the diagnostic path naming a reserved document scope in a surface
// error (e.g. "documentScopes.$theme"), mirroring how item surfaces name an
// instance path. It locates the offending scope declaration on the document schema.
func scopePath(keyword string) string {
	return documentScopesKey + "." + keyword
}
