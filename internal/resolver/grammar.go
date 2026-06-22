package resolver

// This file implements the DASHBOARD TREE GRAMMAR pass (E3-S2): a single,
// fail-fast walk over the assembled resolved tree that enforces the structural
// shape the dashboard must obey BEYOND per-instance schema validation. Where each
// kind of node may appear is a tree-shape rule no single item-type schema can
// express, so it lives here as one pass invoked once from resolver.go.
//
// The grammar (FR-A1, FR-A2, FR-A5, FR-A6, FR-A8):
//
//   - root holds ONLY positional REGION types. A region is any type carrying the
//     schema-level `positional` marker (E3-S1) — initially `container` and
//     `variable-box`; the marker is the SINGLE SOURCE OF TRUTH, so adding a new
//     region type needs no edit here. A content leaf or a block wrapper directly
//     under root fails (GRAMMAR_ROOT_CHILD_INVALID).
//
//   - a region whose `latticeBehavior.childPolicy` is `regions-or-wrappers` may
//     nest other positional regions OR hold block wrappers; a bare (unwrapped)
//     content leaf under such a region fails (GRAMMAR_REGION_CHILD_INVALID) —
//     content must be block-wrapped. `container` is the built-in example.
//
//   - a region whose `latticeBehavior.childPolicy` is `widgets` holds ONLY
//     variable widgets, DIRECTLY (not wrapped, not nested in a region). Any
//     non-widget child fails (GRAMMAR_VARIABLE_BOX_CHILD_INVALID). The built-in
//     `variable-box` and the collapsed `form` (E3-S2) both declare this policy;
//     the box/form, not a per-widget wrapper, supplies the grouped presentation.
//
//   - a block `wrapper` holds exactly one CONTENT leaf and never re-wraps a
//     wrapper. The exactly-one-content invariant is already enforced fail-fast by
//     the block pass (WRAPPER_CHILD_COUNT_INVALID, block.go); this pass adds the
//     no-recursion rule: a wrapper whose single inner node is itself a wrapper
//     fails (GRAMMAR_WRAPPER_NESTED).
//
//   - a positional REGION carries NO theme of its own (regions are layout-only;
//     only wrappers carry chrome). The positional schemas already forbid a `theme`
//     field structurally (additionalProperties:false), but this pass also rejects
//     a theme appearing on a region node so the violation surfaces with a clear,
//     grammar-specific code (GRAMMAR_REGION_THEME_FORBIDDEN).
//
// Node-kind identity is taken from the catalog/type metadata, NOT a hardcoded type
// list where avoidable: "positional region" reads the E3-S1 `positional` marker
// (via ResolvedType.IsPositional); a region's child rule is selected by its
// `latticeBehavior.childPolicy` keyword (via ResolvedType.ChildPolicy, E3-S3), NOT
// by item-type name — so the collapsed `form` and any custom `widgets` region get
// the right rule for free; "wrapper" is the block item-type name; "variable widget"
// reads the `latticeBehavior.role == "widget"` keyword (via ResolvedType.Role,
// E2-S1) against the graph's type table — see isVariableWidget in widget.go. The
// widget judgment is now schema-keyword driven, no name list.
//
// The pass runs AFTER the instance walk because it needs the whole assembled tree
// and each node's resolved type identity. It is fail-fast: the FIRST violation
// stops the walk and is returned as a CodedError naming the offending path. It
// builds NO per-check index — the marker lookup is O(1) against the graph's
// already-built type table (g.Types), so the walk is a single pass with no
// re-walking. It lives in its own file and is invoked by a single call from
// resolver.go's resolveBytes.

import (
	"strconv"

	"github.com/frankbardon/lattice/errors"
	"github.com/frankbardon/lattice/internal/schema"
)

// themeKey is the reserved config field a positional region must NOT carry. Only
// block wrappers declare a theme override (block.go's reserved keys); a region is
// layout-only. The grammar pass rejects a theme appearing on a region node.
const themeKey = "theme"

// resolveGrammar walks the assembled resolved tree once and enforces the dashboard
// tree grammar, fail-fast. g supplies the resolved type table so node kinds are
// decided from the schema-level `positional` marker (index-once: the table is
// already built; this pass adds no second walk to discover kinds). The root itself
// is the document root and is not grammar-checked as a child of anything; its
// children are checked against the root rule, and the walk recurses region-by-region.
func resolveGrammar(g *schema.ResolvedGraph, root *ResolvedInstance) error {
	// The root node is the implicit positional region of the document; like any
	// region it carries no theme of its own.
	if err := checkRegionNoTheme(root, "root"); err != nil {
		return err
	}
	for i, child := range root.Children {
		childPath := "root.children[" + strconv.Itoa(i) + "]"
		if !isPositionalRegion(g, child) {
			return errors.NewCodedErrorWithDetails(errors.GRAMMAR_ROOT_CHILD_INVALID,
				"root accepts only positional region types (e.g. container, variable-box)",
				map[string]any{"path": childPath, "type": child.Type.Name})
		}
		if err := checkRegion(g, child, childPath); err != nil {
			return err
		}
	}
	return nil
}

// checkRegion validates one positional region node and recurses. It dispatches on
// the region's `latticeBehavior.childPolicy` (E3-S3), NOT its item-type name: a
// region declaring `widgets` accepts only variable widgets held directly; one
// declaring `regions-or-wrappers` accepts nested regions or block wrappers. A
// region carries no theme of its own.
func checkRegion(g *schema.ResolvedGraph, region *ResolvedInstance, path string) error {
	if err := checkRegionNoTheme(region, path); err != nil {
		return err
	}
	if regionChildPolicy(g, region) == schema.ChildPolicyWidgets {
		return checkWidgetsRegion(g, region, path)
	}
	return checkRegionsOrWrappersRegion(g, region, path)
}

// checkRegionsOrWrappersRegion validates a `regions-or-wrappers` region's children:
// each must be another positional region (a nested sub-layout) OR a block wrapper.
// A bare content leaf fails — content must be block-wrapped. Recurses into nested
// regions and into wrappers. `container` is the built-in example.
func checkRegionsOrWrappersRegion(g *schema.ResolvedGraph, region *ResolvedInstance, path string) error {
	for i, child := range region.Children {
		childPath := path + ".children[" + strconv.Itoa(i) + "]"
		switch {
		case isPositionalRegion(g, child):
			if err := checkRegion(g, child, childPath); err != nil {
				return err
			}
		case isWrapper(child):
			if err := checkWrapper(child, childPath); err != nil {
				return err
			}
		default:
			return errors.NewCodedErrorWithDetails(errors.GRAMMAR_REGION_CHILD_INVALID,
				"a container holds nested regions or block wrappers; a bare content leaf must be wrapped in a block",
				map[string]any{"path": childPath, "type": child.Type.Name})
		}
	}
	return nil
}

// checkWidgetsRegion validates a `widgets`-policy region's children: every child
// must be a variable widget held directly (not wrapped, not a nested region). Such
// a region (built-in `variable-box`, collapsed `form`) is the dedicated, leaf-only
// home for the widget family; widgets are leaves and carry no children of their
// own, so there is nothing to recurse into. The error code is unchanged
// (GRAMMAR_VARIABLE_BOX_CHILD_INVALID) — it now fires for any `widgets` region, not
// only variable-box.
func checkWidgetsRegion(g *schema.ResolvedGraph, region *ResolvedInstance, path string) error {
	for i, child := range region.Children {
		childPath := path + ".children[" + strconv.Itoa(i) + "]"
		if !isVariableWidget(g, child) {
			return errors.NewCodedErrorWithDetails(errors.GRAMMAR_VARIABLE_BOX_CHILD_INVALID,
				"a variable-box holds only variable widgets, directly (not wrapped, not nested)",
				map[string]any{"path": childPath, "type": child.Type.Name})
		}
	}
	return nil
}

// checkWrapper enforces the wrapper's grammar concern: no recursion. A block holds
// exactly one inner CONTENT leaf (the exactly-one count is guaranteed by the block
// pass before this runs), and that inner node must not itself be a wrapper. The
// inner content is a plain leaf, so there is nothing further to recurse into.
func checkWrapper(wrapper *ResolvedInstance, path string) error {
	for i, child := range wrapper.Children {
		childPath := path + ".children[" + strconv.Itoa(i) + "]"
		if isWrapper(child) {
			return errors.NewCodedErrorWithDetails(errors.GRAMMAR_WRAPPER_NESTED,
				"a block wrapper holds exactly one content leaf and may not wrap another wrapper",
				map[string]any{"path": childPath})
		}
	}
	return nil
}

// checkRegionNoTheme rejects a positional region node that carries a `theme`
// element. Regions are layout-only; only wrappers carry theme. The positional
// schemas forbid theme structurally, so reaching here means the marker/schema
// disagree — this is the grammar-level guard with a clear code.
func checkRegionNoTheme(region *ResolvedInstance, path string) error {
	if region.Config == nil {
		return nil
	}
	if _, ok := region.Config[themeKey]; ok {
		return errors.NewCodedErrorWithDetails(errors.GRAMMAR_REGION_THEME_FORBIDDEN,
			"a positional region carries no theme; only block wrappers carry chrome",
			map[string]any{"path": path, "type": region.Type.Name})
	}
	return nil
}

// isPositionalRegion reports whether a resolved node's item type carries the
// schema-level `positional` marker (E3-S1), making it a legal positional region.
// The marker is read from the graph's already-built type table by the node's
// canonical type id, so this is an O(1) lookup, not a re-walk.
func isPositionalRegion(g *schema.ResolvedGraph, inst *ResolvedInstance) bool {
	if g == nil {
		return false
	}
	return g.Types[inst.Type.ID].IsPositional()
}

// regionChildPolicy reports a region node's declared child-admission policy
// (E3-S3), read O(1) from the graph's already-built type table by the node's
// canonical type id — the same source as isPositionalRegion. This selects the
// region's grammar rule WITHOUT a name check, so the collapsed form and any custom
// region inherit the right rule from their schema's `latticeBehavior.childPolicy`.
func regionChildPolicy(g *schema.ResolvedGraph, inst *ResolvedInstance) schema.ChildPolicy {
	if g == nil {
		return schema.ChildPolicyNone
	}
	return g.Types[inst.Type.ID].ChildPolicy()
}

// isWrapper reports whether a resolved node is a block wrapper (by item-type
// name). Wrappers are the only content carriers under a container and the only
// nodes the no-recursion rule applies to.
func isWrapper(inst *ResolvedInstance) bool {
	return inst.Type.Name == blockTypeName
}
