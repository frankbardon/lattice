package resolver

// This file implements the CONFIGURATOR pass: the mechanism by which a
// `configurator` item references ANOTHER item in the same document — its
// `target` — by that item's stable instance `id`, the resolver validates that
// the reference resolves to a real, id-carrying item (E5-S1), AND, when it does,
// AUTO-GENERATES an editor form from the target's configurable surface (E5-S2).
//
// E5-S1 (TYPE + TARGET VALIDATION): confirms every configurator points at an item
// that actually exists in the tree and carries a stable id.
//
// E5-S2 (FORM AUTO-GENERATION): once the target resolves, the pass reads the
// target's validated configurable surface (ResolvedInstance.Surface, attached by
// the E3 surface pass which runs BEFORE this one) and generates ONE widget per
// surface field — using the field's preferred `rendering` if present, else the
// CANONICAL widget for the field's value type — laid out via the form FLOW layout
// (E2-S1, layout.NormalizeFlow). Each generated widget carries the override
// BINDING it drives: the target's id + the field, so serve can post the right
// `<target-id>.<field>` config override (E4-S2) and re-resolve the target
// ephemerally. The generated form is attached to the configurator node
// (ResolvedInstance.Generated). Generation is PURE auto — there is no per-field
// authoring on the configurator.
//
// KNOWN LIMITATION (carried): the surface mechanism is top-level-only, so a
// container/table/form exposes a composite field (grid/columns/query) as a SINGLE
// surface entry. The configurator therefore generates ONE widget per surface
// entry (e.g. one widget for the whole `columns` array), not one per sub-field.
// Per-sub-field editing is a future story.
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
	"github.com/frankbardon/lattice/internal/layout"
	"github.com/frankbardon/lattice/internal/variables"
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

// resolveTarget validates a single configurator's `target` against the id index
// and, on success, auto-generates the editor form from the target's configurable
// surface (E5-S2). An empty/whitespace-only target fails fast with
// CONFIGURATOR_TARGET_MISSING_ID (the reference carries no stable id to look up);
// a well-formed target that no item declares fails fast with
// CONFIGURATOR_TARGET_NOT_FOUND. A resolved target's surface is generated into a
// GeneratedForm attached to the configurator node.
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

	targetNode, found := index[target]
	if !found {
		return errors.NewCodedErrorWithDetails(errors.CONFIGURATOR_TARGET_NOT_FOUND,
			"configurator target does not match any item id in the document",
			map[string]any{"path": path, configuratorTargetKey: target})
	}

	// E5-S2: auto-generate the editor form from the resolved target's configurable
	// surface and attach it to the configurator node. A surface-less target yields
	// an empty (but present) form, so a renderer can always distinguish a resolved
	// configurator from an unresolved one.
	form, err := generateForm(target, targetNode, path)
	if err != nil {
		return err
	}
	inst.Generated = form
	return nil
}

// generateForm builds the auto-generated editor form for a configurator whose
// target resolved to targetNode. It emits one GeneratedWidget per configurable
// surface field of the target (in surface/sorted order), choosing the field's
// preferred `rendering` when present or the canonical widget for the field's
// value type otherwise, and binds each widget to the `<target-id>.<field>`
// config-override address. The widgets are laid out via the form FLOW layout
// (E2-S1's layout.NormalizeFlow), so they arrange exactly like an authored form's
// controls. path is the configurator's instance path, embedded in any layout
// error. Generation cannot fail on a well-formed surface; the only error path is
// a degenerate flow (defended for parity with the form pass).
func generateForm(targetID string, targetNode *ResolvedInstance, path string) (*GeneratedForm, error) {
	widgets := make([]GeneratedWidget, 0, len(targetNode.Surface))
	for _, f := range targetNode.Surface {
		widgets = append(widgets, GeneratedWidget{
			Widget:      widgetForField(f),
			Target:      targetID,
			Field:       f.Field,
			Type:        f.Type,
			Label:       widgetLabel(f),
			Constraints: f.Constraints,
		})
	}

	// Reuse the form flow layout (E2-S1): a single column of stacked label+control
	// rows, one cell per generated widget. The generated form has no authored
	// `columns`, so it flows into the default single column.
	flow, err := layout.NormalizeFlow(layout.FlowConfig{}, len(widgets), path)
	if err != nil {
		return nil, err
	}

	return &GeneratedForm{
		Target:  targetID,
		Widgets: widgets,
		Flow:    flow,
	}, nil
}

// canonicalWidget maps a configurable field's value type to the canonical widget
// item-type that edits it when the surface declares no preferred rendering. The
// canonical choice mirrors the widget catalog (widget.go): string→text-input,
// number/integer→number-field, boolean→toggle, enum→select, array→multiselect.
// Every value is a registered widget family.
var canonicalWidget = map[variables.VarType]string{
	variables.VarTypeString:  "text-input",
	variables.VarTypeNumber:  "number-field",
	variables.VarTypeInteger: "number-field",
	variables.VarTypeBoolean: "toggle",
	variables.VarTypeEnum:    "select",
	variables.VarTypeArray:   "multiselect",
}

// widgetForField selects the widget item-type that renders a surface field's
// editor: the field's preferred `rendering` when the surface declared one (the
// surface pass has already validated it names a registered widget family), else
// the canonical widget for the field's value type. The field's value type is one
// of the variable type set (validated by the surface pass), so the canonical
// lookup always hits.
func widgetForField(f ConfigurableField) string {
	if f.Rendering != "" {
		return f.Rendering
	}
	return canonicalWidget[f.Type]
}

// widgetLabel is the human label rendered for a generated widget: the surface
// field's declared label, falling back to the field name when the surface
// declared none, so a control is never label-less.
func widgetLabel(f ConfigurableField) string {
	if f.Label != "" {
		return f.Label
	}
	return f.Field
}
