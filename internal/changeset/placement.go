package changeset

// This file owns PLACEMENT-EDIT CLASSIFICATION: the rule that recognizes a
// changeset op targeting the `placement` envelope sibling so the apply pass can
// route it PAST the field-edit surface guardrail, the same way it routes a
// metadata edit (metadata.go) or a structural edit (structural.go).
//
// WHY PLACEMENT BYPASSES THE SURFACE. `placement` is an instance ENVELOPE field
// (a sibling of `config`/`children`/`metadata`), NOT a configurable-surface
// field — the resolver never lists it on a node's ConfigurableField surface (the
// grid lives on the CONTAINER's surface as `config.grid`; a child's own placement
// is a plain envelope member). So a placement edit can never match a surface entry
// and, left to checkOp, would be rejected as off-surface
// (CONFIG_OVERRIDE_FIELD_UNKNOWN) — exactly the engine/docs mismatch this fixes:
// placement-grid.md documents `/<node-id>/placement/colStart` as a valid field
// edit. Like a metadata or structural edit, it is instead gated by RE-RESOLVE: the
// pipeline re-runs the full resolver over the mutated document, which re-runs the
// layout pass (internal/resolver/layout.go -> internal/layout.Normalize) and
// enforces placement INTEGER form (LAYOUT_PLACEMENT_INVALID) and GRID BOUNDS
// (LAYOUT_PLACEMENT_OUT_OF_BOUNDS) against the parent container's grid. An op
// pushing a coordinate off the declared tracks is therefore rejected by re-resolve
// with no persistence.
//
// WHAT THIS COVERS. add / replace / remove on both the WHOLE-OBJECT target
// (`/<id>/placement`) and a SINGLE-COORDINATE target
// (`/<id>/placement/colStart|colSpan|rowStart|rowSpan`), for an item node
// (`<id>`). The id-rooted pointer translates verbatim (translate.go): a
// placement edit follows the item's physical base and appends the literal
// `placement[/<coord>]` remainder. No apply-time check is owed — unlike a
// structural add, a placement write supplies no id contract that re-resolve
// cannot supply.

import "strings"

// isPlacementEdit reports whether an OP targets the `placement` envelope — so the
// apply pass routes it past the field-edit surface guardrail and relies on
// re-resolve to enforce the integer form and grid bounds (the layout pass). It is
// the op-aware front of isPlacementPointer: a `move`/`copy` whose EITHER endpoint
// addresses a placement location relocates into/out of the envelope and is likewise
// off-surface, so the op is a placement edit if either pointer is one. add / remove /
// replace / test classify on their single `path`.
func isPlacementEdit(op Operation) bool {
	if isPlacementPointer(op.Path) {
		return true
	}
	if op.HasFrom {
		return isPlacementPointer(op.From)
	}
	return false
}

// isPlacementPointer reports whether an id-rooted POINTER addresses the `placement`
// envelope of an item node. A pointer is a placement pointer when its remainder
// past the leading id is rooted at `placement` — the whole object (`placement`) or
// a single coordinate (`placement/colStart` and the other coordinate keys, which
// re-resolve validates). The leading segment is an item id (`/<id>/placement`); a
// config edit (remainder rooted at `config`), a metadata edit (`metadata`), and a
// structural edit (`children`) are NOT placement pointers. A malformed pointer is
// left for the translator to reject with its own coded error.
func isPlacementPointer(pointer string) bool {
	if pointer == "" || pointer[0] != '/' {
		return false
	}
	leading, remainder, _ := strings.Cut(pointer[1:], "/")
	if leading == "" || remainder == "" {
		return false
	}
	seg, _, _ := strings.Cut(remainder, "/")
	return seg == placementKey
}
