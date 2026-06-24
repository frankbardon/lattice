package changeset

// This file owns VARIABLES-EDIT CLASSIFICATION: the rule that recognizes a
// changeset op targeting the document `$variables` scope (the variable-declaration
// array) so the apply pass can route it PAST the field-edit surface guardrail, the
// same way it routes a metadata edit (metadata.go), a placement edit
// (placement.go), or a structural edit (structural.go).
//
// WHY $variables BYPASSES THE SURFACE. The settable `$`-scope surfaces are only
// `$manifest` and `$theme` (apply.go resolveScopeTarget); `$variables` has no
// configurable surface, so a variable edit could never match a surface entry and,
// left to checkOp, would be rejected as off-surface (CONFIG_OVERRIDE_FIELD_UNKNOWN).
// Yet the variable-declaration array IS a legitimate, document-scoped edit target
// (the translator already maps `$variables` -> `/variables`, translate.go), and the
// resolver's VARIABLES PASS (internal/variables) fully validates it. Like a
// metadata, placement, or structural edit, a `$variables` edit is therefore gated
// by RE-RESOLVE: the pipeline re-runs the full resolver over the mutated document,
// which re-runs the variables pass and rejects an invalid declaration
// (VAR_DECLARATION_INVALID), a duplicate name, or a bad default/option with no
// persistence.
//
// WHAT THIS COVERS. add / replace / remove / test on the `$variables` scope —
// the whole array (`/$variables`), an append (`/$variables/-`), a single
// declaration (`/$variables/<i>`), or a field within one (`/$variables/<i>/default`,
// `/$variables/<i>/options/-`, …). The id-rooted pointer translates verbatim
// (translate.go) onto the physical `/variables` base. No apply-time check is owed —
// unlike a structural add, a variables edit supplies no id contract re-resolve
// cannot supply.

import "strings"

// variablesScope is the reserved `$`-scope keyword addressing the document's
// variable-declaration array. It mirrors reservedScopeBase's `$variables` key
// (translate.go), kept local so the bypass classifier does not depend on the map.
const variablesScope = "$variables"

// isVariablesEdit reports whether an OP targets the `$variables` scope — so the
// apply pass routes it past the field-edit surface guardrail and relies on
// re-resolve to enforce variable-declaration validity (the variables pass). It is
// the op-aware front of isVariablesPointer: a `move`/`copy` whose EITHER endpoint
// addresses the variables scope moves a value into/out of it and is likewise
// off-surface, so the op is a variables edit if either pointer is one. add /
// remove / replace / test classify on their single `path`.
func isVariablesEdit(op Operation) bool {
	if isVariablesPointer(op.Path) {
		return true
	}
	if op.HasFrom {
		return isVariablesPointer(op.From)
	}
	return false
}

// isVariablesPointer reports whether an id-rooted POINTER addresses the
// `$variables` scope. A pointer is a variables pointer when its leading segment is
// the `$variables` keyword — whether the scope as a whole (`/$variables`) or any
// path within it (`/$variables/-`, `/$variables/0/default`, …). A config edit, a
// metadata/placement edit, and a structural `children` edit are NOT variables
// pointers (their leading segment is an item id or a different scope).
func isVariablesPointer(pointer string) bool {
	if pointer == "" || pointer[0] != '/' {
		return false
	}
	leading, _, _ := strings.Cut(pointer[1:], "/")
	return leading == variablesScope
}
