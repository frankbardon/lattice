package changeset

// This file owns METADATA-EDIT CLASSIFICATION (element-metadata E2-S1): the rule
// that recognizes a changeset op targeting the `metadata` envelope sibling so the
// apply pass can route it PAST the field-edit surface guardrail, the same way it
// routes a structural edit (structural.go).
//
// WHY METADATA BYPASSES THE SURFACE. `metadata` is a passthrough envelope field
// (a sibling of `config`/`children`/`placement`), NOT a configurable-surface
// field — the resolver never lists it on a node's ConfigurableField surface. So a
// metadata edit can never match a surface entry and, left to checkOp, would be
// rejected as off-surface (CONFIG_OVERRIDE_FIELD_UNKNOWN). Like a structural edit,
// it is instead gated by RE-RESOLVE: the pipeline re-runs the full resolver over
// the mutated document, which enforces metadata ELIGIBILITY (only the document
// root, regions-or-wrappers containers, and block wrappers may carry metadata —
// METADATA_NOT_ELIGIBLE) and the SCALAR-VALUE rule (METADATA_VALUE_NOT_SCALAR),
// keyed on the `latticeBehavior` accessors (internal/resolver/metadata.go, E1-S2).
// An op placing metadata on an ineligible node or setting a non-scalar value is
// therefore rejected by re-resolve with no persistence.
//
// WHAT THIS COVERS. add / replace / remove on both a WHOLE-OBJECT target
// (`/<id>/metadata`) and a SINGLE-KEY target (`/<id>/metadata/<key>`), for an item
// node (`<id>`) and the document root (`/$root/metadata[/<key>]` — the root
// instance, whose metadata the resolver threads together with the top-level
// document `metadata` sibling). The id-rooted pointer translates verbatim
// (translate.go): `/<id>/metadata` follows the item's physical base, and
// `/$root/metadata` follows the `$root` scope base (`/root`). No apply-time check
// is owed — unlike a structural add, a metadata write supplies no id contract that
// re-resolve cannot supply.

import "strings"

// metadataKey is the instance ENVELOPE member holding a node's freeform passthrough
// metadata (a sibling of `config`/`children`/`placement`, NOT a config field). A
// metadata edit's id-rooted pointer remainder is rooted at this key
// (`/<id>/metadata`, `/<id>/metadata/<key>`); a config edit is rooted at `config`
// and a structural edit at `children` instead.
const metadataKey = "metadata"

// isMetadataEdit reports whether an OP targets the `metadata` envelope — so the
// apply pass routes it past the field-edit surface guardrail and relies on
// re-resolve to enforce eligibility + the scalar-value rule (E1-S2). It is the
// op-aware front of isMetadataPointer: a `move`/`copy` whose EITHER endpoint
// addresses a metadata location relocates into/out of the envelope and is likewise
// off-surface, so the op is a metadata edit if either pointer is one. add / remove /
// replace / test classify on their single `path`.
func isMetadataEdit(op Operation) bool {
	if isMetadataPointer(op.Path) {
		return true
	}
	if op.HasFrom {
		return isMetadataPointer(op.From)
	}
	return false
}

// isMetadataPointer reports whether an id-rooted POINTER addresses the `metadata`
// envelope of an item or the document root. A pointer is a metadata pointer when its
// remainder past the leading id/scope is rooted at `metadata` — the whole object
// (`metadata`) or a single key (`metadata/<key>`, and any deeper key path, which
// re-resolve validates). The leading segment may be an item id (`/<id>/metadata`) or
// the `$root` document scope (`/$root/metadata`); a config edit (remainder rooted at
// `config`) and a structural edit (rooted at `children`) are NOT metadata pointers. A
// malformed pointer is left for the translator to reject with its own coded error.
func isMetadataPointer(pointer string) bool {
	if pointer == "" || pointer[0] != '/' {
		return false
	}
	leading, remainder, _ := strings.Cut(pointer[1:], "/")
	if leading == "" || remainder == "" {
		return false
	}
	seg, _, _ := strings.Cut(remainder, "/")
	return seg == metadataKey
}
