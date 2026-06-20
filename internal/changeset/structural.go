package changeset

// This file owns STRUCTURAL `$root` EDITING (E3-S1): the classification that
// separates a STRUCTURAL changeset op (one that reshapes the tree by inserting or
// deleting an item in a `children` array) from a FIELD/CONFIG edit, and the
// apply-layer validation a structural ADD needs that re-resolve cannot supply.
//
// TWO GUARDRAILS, ONE APPLY PASS. A field/config edit is governed by the
// CONFIGURABLE SURFACE (apply.go's surfaceIndex): it may only touch a field the
// resolver exposes as settable. A structural edit is governed instead by
// RE-RESOLVE — the pipeline re-runs the full resolver over the mutated document, so
// the tree grammar (internal/resolver/grammar.go: root holds only positional
// regions, a container holds regions or block wrappers, a bare leaf must be
// wrapped, …) and the per-item schema re-validate the new shape "for free." The
// `$root` configurable surface is intentionally EMPTY, which is exactly why a
// structural edit cannot be surface-gated: there is no surface field to match. So
// the apply pass must DISTINGUISH the two and route each to its own guardrail — a
// structural op MUST bypass the field-edit surface check (or it would be rejected
// as off-surface) and is validated by re-resolve, while a field edit MUST stay
// surface-gated.
//
// WHAT A STRUCTURAL ADD STILL NEEDS HERE. Re-resolve catches an illegal shape, an
// invalid child type, a dangling ref, and a schema-invalid config. It does NOT
// catch a missing or DUPLICATE instance id: the resolver's id index is pre-order,
// last-wins (internal/resolver/configurator.go buildIDIndex), so a second node
// reusing an existing id is silently shadowed, not rejected. The id is also the
// stable address every changeset pointer roots on, so a missing/colliding id would
// corrupt addressing. This file therefore validates, against the document being
// patched, that an added item carries its own non-empty, document-unique `id`
// BEFORE the op is applied (CHANGESET_STRUCTURAL_ID_INVALID); any failure rejects
// the whole changeset and persists nothing.

import (
	"encoding/json"
	"strings"

	"github.com/frankbardon/lattice/errors"
)

// childrenKey is the instance member holding a node's positional child slots —
// the only structural insertion/deletion site in the tree (containers and forms
// carry it). A structural op's id-rooted pointer remainder is rooted at this key
// (`/<id>/children/-`, `/<id>/children/N`); a config edit is rooted at `config`
// instead. Block inner content lives at `config/content`, NOT here, so it is a
// field edit, not a structural one (the grammar treats the wrapper's single
// content leaf as config, not a child slot).
const childrenKey = "children"

// idKey is the instance member carrying a node's stable, document-unique
// identifier — the address every id-rooted changeset pointer roots on. A
// structural add's value MUST supply it (presence + uniqueness checked here);
// re-resolve does not, since the resolver's id index is last-wins on collision.
const idKey = "id"

// isStructuralOp reports whether an id-rooted op is a STRUCTURAL edit — one that
// reshapes the tree rather than editing a field — so the apply pass can route it
// past the field-edit surface guardrail and rely on re-resolve instead. An op is
// structural when its pointer addresses a `children` array (insert/delete a child:
// remainder rooted at `children`) OR addresses an item/scope ROOT as a whole
// (remainder empty: the item itself, the remove-by-item-id form). A config edit
// (remainder rooted at `config`) is NOT structural and stays surface-gated. A
// malformed pointer is left for checkOp/the translator to reject with its own
// coded error, so this classifier never has the last word on a bad pointer.
func isStructuralOp(pointer string) bool {
	if pointer == "" || pointer[0] != '/' {
		return false
	}
	leading, remainder, _ := strings.Cut(pointer[1:], "/")
	if leading == "" {
		return false
	}
	// The item/scope addressed as a whole — the remove-by-item-id form
	// (`/<item-id>`) and the append/positional forms below both reshape the tree.
	if remainder == "" {
		return true
	}
	// A `children` array or one of its elements: `children`, `children/-`,
	// `children/N`. Any deeper `children/...` path is still structural (it targets a
	// node inside a child slot), which re-resolve validates.
	seg, _, _ := strings.Cut(remainder, "/")
	return seg == childrenKey
}

// checkStructuralOp validates a structural op the apply layer can check ahead of
// re-resolve. Today that is the ADD-id contract: an `add` op inserting a full item
// instance must carry a non-empty `id` that is unique across the document being
// patched. remove/move/copy and any add NOT targeting a `children` array carry no
// such obligation here (re-resolve owns their legality); they pass through. doc is
// the decoded document the op will be applied to, used to detect an id collision.
func checkStructuralOp(op Operation, doc map[string]any, opIndex int) error {
	// Only an `add` that inserts a NEW node (into a `children` array) supplies an
	// instance whose id must be present and unique. A structural remove names an
	// existing node by its (already-unique) id; a `children`-targeted add inserting
	// a non-instance is left to re-resolve/the applier.
	if op.Op != OpAdd || !addsIntoChildren(op.Path) {
		return nil
	}
	return validateAddedInstanceID(op, doc, opIndex)
}

// addsIntoChildren reports whether an add op's pointer targets a `children` array
// or one of its slots (`/<id>/children/-`, `/<id>/children/N`) — the case where
// the value is a full item instance whose id must be validated. An add addressing
// an item/scope root as a whole (remainder empty) is not an instance insertion in
// this story (it would replace a whole subtree); it is left to re-resolve.
func addsIntoChildren(pointer string) bool {
	if pointer == "" || pointer[0] != '/' {
		return false
	}
	_, remainder, _ := strings.Cut(pointer[1:], "/")
	seg, _, _ := strings.Cut(remainder, "/")
	return seg == childrenKey
}

// validateAddedInstanceID enforces the structural-add id contract against the
// document being patched: the op's value must be a JSON object carrying a
// non-empty string `id` that no node already declares. A non-object value, a
// missing/blank/non-string id, or a collision with an existing id is rejected with
// CHANGESET_STRUCTURAL_ID_INVALID, naming the pointer and (when present) the id.
func validateAddedInstanceID(op Operation, doc map[string]any, opIndex int) error {
	if !op.HasValue {
		// Parse already requires `value` for add; this is a defensive guard.
		return structuralIDError("structural add op has no value to insert", op.Path, "", opIndex)
	}

	var value map[string]any
	if err := json.Unmarshal(op.Value, &value); err != nil {
		return structuralIDError(
			"structural add value is not a JSON object carrying an item instance", op.Path, "", opIndex)
	}

	id, ok := value[idKey].(string)
	if !ok || id == "" {
		return structuralIDError(
			"structural add value is missing its required non-empty string `id`", op.Path, "", opIndex)
	}

	if documentHasID(doc, id) {
		return structuralIDError(
			"structural add value's `id` collides with an item already in the document", op.Path, id, opIndex)
	}
	return nil
}

// structuralIDError builds the CHANGESET_STRUCTURAL_ID_INVALID coded error,
// carrying the offending pointer, the supplied id (when known), and the op index
// into Details for diagnostics.
func structuralIDError(msg, pointer, id string, opIndex int) error {
	details := map[string]any{"pointer": pointer, "index": opIndex}
	if id != "" {
		details["id"] = id
	}
	return errors.NewCodedErrorWithDetails(errors.CHANGESET_STRUCTURAL_ID_INVALID, msg, details)
}

// documentHasID reports whether any instance in the decoded document already
// declares id. It walks the SAME physical instance-bearing locations the
// translator indexes — the root, each `children[i]` slot, and a block wrapper's
// `config/content` leaf — so uniqueness is checked over exactly the set of
// id-carrying nodes the pointer dialect can address. The walk is shape-led (it
// descends wherever a nested instance can physically live) and does not resolve
// refs.
func documentHasID(doc map[string]any, id string) bool {
	if doc == nil {
		return false
	}
	root, ok := doc["root"].(map[string]any)
	if !ok {
		return false
	}
	return instanceHasID(root, id)
}

// instanceHasID reports whether inst or any nested instance under it declares id,
// descending into its block-content leaf and its child slots (the physical
// instance-bearing locations).
func instanceHasID(inst map[string]any, id string) bool {
	if got, ok := inst[idKey].(string); ok && got == id {
		return true
	}
	if cfg, ok := inst[configKey].(map[string]any); ok {
		if content, ok := cfg[instanceContentKey].(map[string]any); ok {
			if instanceHasID(content, id) {
				return true
			}
		}
	}
	if children, ok := inst[childrenKey].([]any); ok {
		for _, child := range children {
			childInst, ok := child.(map[string]any)
			if !ok {
				continue
			}
			if instanceHasID(childInst, id) {
				return true
			}
		}
	}
	return false
}
