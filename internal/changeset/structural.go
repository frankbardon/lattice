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

// placementKey is the instance member carrying a node's explicit grid placement
// ({colStart,colSpan,rowStart,rowSpan}), interpreted in its IMMEDIATE PARENT
// container's grid coordinates (internal/layout). Because it is parent-relative, a
// cross-parent move makes it stale; the move fix-up strips it so the node defaults
// to the first cell of its new parent's grid.
const placementKey = "placement"

// isStructuralEdit reports whether an OP is a STRUCTURAL edit — one that reshapes
// the tree rather than editing a field — so the apply pass can route it past the
// field-edit surface guardrail and rely on re-resolve instead. It is the op-aware
// front of isStructuralPointer: a `move`/`copy` carries BOTH a `from` and a `path`
// (E3-S2: reorder within a parent, move across parents), and relocating a tree
// NODE is structural even when only one end addresses a `children` slot, so the op
// is structural if EITHER pointer is structural. add/remove/replace/test reshape
// (or not) through their single `path` alone.
func isStructuralEdit(op Operation) bool {
	if isStructuralPointer(op.Path) {
		return true
	}
	// move/copy relocate a node named by `from`; classify on the source pointer too
	// so a reorder/move whose `from` is an item root (`/<item-id>`) routes
	// structurally and is gated by re-resolve, never by the (empty) `$root` surface.
	if op.HasFrom {
		return isStructuralPointer(op.From)
	}
	return false
}

// isStructuralPointer reports whether an id-rooted POINTER addresses a STRUCTURAL
// location — one that reshapes the tree rather than naming a field. A pointer is
// structural when it addresses a `children` array (insert/delete/reorder a child:
// remainder rooted at `children`) OR addresses an item/scope ROOT as a whole
// (remainder empty: the item itself, the remove-by-item-id and move-from forms). A
// config edit (remainder rooted at `config`) is NOT structural and stays
// surface-gated. A malformed pointer is left for checkOp/the translator to reject
// with its own coded error, so this classifier never has the last word on a bad
// pointer.
func isStructuralPointer(pointer string) bool {
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

// reconcileMovedPlacements strips the stale `placement` from any node a `move`
// relocated to a DIFFERENT parent. Placement is expressed in the immediate parent
// container's grid coordinates (internal/layout), so a node carried across parents
// by a move keeps coordinates that belong to the OLD grid — which re-resolve's
// layout bounds would reject (LAYOUT_PLACEMENT_OUT_OF_BOUNDS) even when the move is
// otherwise grammar-legal. Dropping the placement lets the node fall back to the
// default first-cell placement (internal/layout.normalizePlacement), which fits any
// grid, so a legal cross-parent move re-resolves. A SAME-parent reorder keeps its
// placement: the grid is unchanged, so the explicit coordinates are still valid and
// the author's placement must be preserved.
//
// before is the document as decoded BEFORE the patch; mutated is the applier's
// output bytes AFTER it. It compares each moved node's parent-id in the two trees
// and rewrites only when the parent changed. A non-move op contributes nothing. The
// fix-up is keyed on the moved node's id (the `from` pointer's leading segment),
// which the structural-move contract requires to be a stable item id.
func reconcileMovedPlacements(before map[string]any, mutated []byte, cs *Changeset) ([]byte, error) {
	movedIDs := movedItemIDs(cs)
	if len(movedIDs) == 0 {
		return mutated, nil
	}

	var after map[string]any
	if err := json.Unmarshal(mutated, &after); err != nil {
		return nil, errors.WrapCodedError(err, errors.RESOLVE_INTERNAL,
			"mutated document is not valid JSON after applying the changeset")
	}

	beforeParents := parentIDByInstanceID(before)
	afterParents := parentIDByInstanceID(after)

	changed := false
	for id := range movedIDs {
		// Only a node still present after the move (its id resolves in `after`) whose
		// parent changed needs its stale placement dropped.
		oldParent, hadOld := beforeParents[id]
		newParent, hasNew := afterParents[id]
		if !hasNew || (hadOld && oldParent == newParent) {
			continue
		}
		if stripPlacementByID(after, id) {
			changed = true
		}
	}
	if !changed {
		return mutated, nil
	}

	out, err := json.Marshal(after)
	if err != nil {
		return nil, errors.WrapCodedError(err, errors.RESOLVE_INTERNAL,
			"failed re-serializing the document after the move placement fix-up")
	}
	return out, nil
}

// movedItemIDs collects the leading item id of every `move` op's `from` pointer —
// the set of nodes a changeset relocates. Only an id-rooted `from` (leading segment
// an item id, not a `$`-scope and not empty) contributes; a `$`-scope or malformed
// `from` is left for the applier/translator, and a non-move op carries no `from` to
// relocate.
func movedItemIDs(cs *Changeset) map[string]struct{} {
	ids := map[string]struct{}{}
	for _, op := range cs.Ops {
		if op.Op != OpMove || !op.HasFrom {
			continue
		}
		from := op.From
		if from == "" || from[0] != '/' {
			continue
		}
		leading, _, _ := strings.Cut(from[1:], "/")
		if leading == "" || strings.HasPrefix(leading, reservedScopePrefix) {
			continue
		}
		ids[leading] = struct{}{}
	}
	return ids
}

// parentIDByInstanceID walks a decoded document and maps each id-carrying instance
// to the id of its nearest id-carrying ANCESTOR instance (its parent in the
// patchable tree). The root has no parent and is recorded with the empty string. It
// descends the same physical instance-bearing locations the translator indexes —
// `children[i]` slots and a block wrapper's `config/content` leaf — so the parent
// relation matches the one a move actually changes.
func parentIDByInstanceID(doc map[string]any) map[string]string {
	parents := map[string]string{}
	if doc == nil {
		return parents
	}
	root, ok := doc["root"].(map[string]any)
	if !ok {
		return parents
	}
	walkParents(root, "", parents)
	return parents
}

// walkParents records inst's id (if any) under parentID, then recurses into inst's
// physical instance-bearing locations with inst's id as the new parent. A node with
// no id is transparent: its children are recorded under the same parentID, so the
// parent relation always links id-carrying nodes.
func walkParents(inst map[string]any, parentID string, parents map[string]string) {
	nextParent := parentID
	if id, ok := inst[idKey].(string); ok && id != "" {
		parents[id] = parentID
		nextParent = id
	}
	if cfg, ok := inst[configKey].(map[string]any); ok {
		if content, ok := cfg[instanceContentKey].(map[string]any); ok {
			walkParents(content, nextParent, parents)
		}
	}
	if children, ok := inst[childrenKey].([]any); ok {
		for _, child := range children {
			if childInst, ok := child.(map[string]any); ok {
				walkParents(childInst, nextParent, parents)
			}
		}
	}
}

// stripPlacementByID finds the instance declaring id and deletes its `placement`
// member, returning whether a placement was actually removed (false when the node
// is absent or carried no placement). It walks the same physical locations as the
// id index; the first matching node is rewritten in place.
func stripPlacementByID(doc map[string]any, id string) bool {
	root, ok := doc["root"].(map[string]any)
	if !ok {
		return false
	}
	target := findInstanceByID(root, id)
	if target == nil {
		return false
	}
	if _, ok := target[placementKey]; !ok {
		return false
	}
	delete(target, placementKey)
	return true
}

// findInstanceByID returns the decoded instance map declaring id, searching inst
// and its physical instance-bearing descendants (block content leaf, child slots),
// or nil if none matches. Pre-order; the first match wins.
func findInstanceByID(inst map[string]any, id string) map[string]any {
	if got, ok := inst[idKey].(string); ok && got == id {
		return inst
	}
	if cfg, ok := inst[configKey].(map[string]any); ok {
		if content, ok := cfg[instanceContentKey].(map[string]any); ok {
			if found := findInstanceByID(content, id); found != nil {
				return found
			}
		}
	}
	if children, ok := inst[childrenKey].([]any); ok {
		for _, child := range children {
			childInst, ok := child.(map[string]any)
			if !ok {
				continue
			}
			if found := findInstanceByID(childInst, id); found != nil {
				return found
			}
		}
	}
	return nil
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
