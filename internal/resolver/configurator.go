package resolver

// This file implements the E5-S1 CONFIGURATOR TARGET-RESOLUTION pass: the
// mechanism by which a `configurator` item references ANOTHER item in the same
// document — its `target` — by that item's stable instance `id`, and the
// resolver validates that the reference resolves to a real, id-carrying item.
//
// Auto-generating the editor form from the target's configurable surface (E3) is
// the NEXT story (E5-S2). This pass adds the TYPE + TARGET VALIDATION only: it
// confirms every configurator points at an item that actually exists in the tree
// and carries a stable id.
//
// Instance `id` is OPTIONAL on the resolved tree (see tree.go) — most items omit
// it. Configurator targeting is the first feature that makes a stable id REQUIRED
// for TARGETED items (an item only needs an id if a configurator points at it).
// To resolve a target the pass builds a tree-wide id index ONCE (id -> node,
// populated only from id-carrying nodes) and looks each target up in it.
//
// Chosen NOT_FOUND vs MISSING_ID semantics (the story leaves this to the
// implementation):
//
//   - CONFIGURATOR_TARGET_MISSING_ID — the configurator's own `target` reference
//     is non-stable: present but empty/whitespace-only. There is no id to look
//     up, so targeting cannot proceed. The item-type schema's minLength guards
//     the empty case structurally; this is the defense-in-depth resolver guard
//     (and also catches a whitespace-only target the schema's minLength accepts).
//
//   - CONFIGURATOR_TARGET_NOT_FOUND — the `target` is a well-formed, non-empty id
//     but NO item in the tree declares it (index miss). The reference dangles.
//
// Because the index is keyed by id and only id-carrying nodes are indexed, a
// successful lookup inherently yields a node with a stable id — there is no case
// where a matched target lacks an id. MISSING_ID therefore describes the
// configurator's end of the reference (an empty target), not the target's.
//
// The pass runs AFTER the instance walk because it needs the whole assembled tree
// to build the id index and to read each configurator's resolved type identity +
// interpolated config. It is fail-fast: the first dangling/empty target stops the
// walk and is returned as a CodedError naming the offending configurator path.
//
// The pass lives in its own file (per file-ownership rules) and is invoked by a
// single call from resolver.go's resolveBytes.

import (
	"strconv"
	"strings"

	"github.com/frankbardon/lattice/errors"
)

// configuratorTypeName is the configurator item-type name. A node whose resolved
// item-type name matches is a configurator and has its `target` validated.
const configuratorTypeName = "configurator"

// configuratorTargetKey is the reserved config key naming the stable instance id
// of the item a configurator generates an editor for. It is required by the
// configurator item-type schema; this pass resolves it against the tree.
const configuratorTargetKey = "target"

// resolveConfigurators walks the assembled resolved tree and validates that every
// configurator's `target` references a real, id-carrying item in the same
// document. It first builds a tree-wide id index ONCE (id -> node), then walks the
// tree resolving each configurator's target against it. It is fail-fast: the first
// configurator with a missing/empty target stops the walk and is returned as a
// CodedError naming the offending configurator path.
func resolveConfigurators(root *ResolvedInstance) error {
	index := buildIDIndex(root)
	return checkConfigurators(root, "root", index)
}

// buildIDIndex collects every id-carrying node of the tree into a single id ->
// node map, built once and shared across the configurator walk. Nodes without a
// stable id are not indexed (only targeted items need an id). When two nodes share
// an id the last one wins; the dashboard schema documents ids as unique within a
// document, so this is a best-effort index, not a uniqueness check.
func buildIDIndex(root *ResolvedInstance) map[string]*ResolvedInstance {
	index := map[string]*ResolvedInstance{}
	var visit func(*ResolvedInstance)
	visit = func(inst *ResolvedInstance) {
		if inst.ID != "" {
			index[inst.ID] = inst
		}
		for _, child := range inst.Children {
			visit(child)
		}
	}
	visit(root)
	return index
}

// checkConfigurators validates one node's target (when it is a configurator) and
// recurses into children.
func checkConfigurators(inst *ResolvedInstance, path string, index map[string]*ResolvedInstance) error {
	if inst.Type.Name == configuratorTypeName {
		if err := resolveTarget(inst, path, index); err != nil {
			return err
		}
	}
	for i, child := range inst.Children {
		childPath := path + ".children[" + strconv.Itoa(i) + "]"
		if err := checkConfigurators(child, childPath, index); err != nil {
			return err
		}
	}
	return nil
}

// resolveTarget validates a single configurator's `target` against the id index.
// An empty/whitespace-only target fails fast with CONFIGURATOR_TARGET_MISSING_ID
// (the reference carries no stable id to look up); a well-formed target that no
// item declares fails fast with CONFIGURATOR_TARGET_NOT_FOUND. A resolved target
// is left as-is — auto-generating the editor from the target's surface is E5-S2.
func resolveTarget(inst *ResolvedInstance, path string, index map[string]*ResolvedInstance) error {
	// The configurator item-type schema requires `target` (a non-empty string), so
	// a structurally-valid configurator always reaches here with a string target.
	// We still read defensively: the schema's minLength does not reject a
	// whitespace-only value, which carries no stable id.
	target, _ := inst.Config[configuratorTargetKey].(string)
	if strings.TrimSpace(target) == "" {
		return errors.NewCodedErrorWithDetails(errors.CONFIGURATOR_TARGET_MISSING_ID,
			"configurator target is empty: targeting requires a stable item id",
			map[string]any{"path": path})
	}

	if _, found := index[target]; !found {
		return errors.NewCodedErrorWithDetails(errors.CONFIGURATOR_TARGET_NOT_FOUND,
			"configurator target does not match any item id in the document",
			map[string]any{"path": path, configuratorTargetKey: target})
	}

	return nil
}
