package resolver

// This file implements the BLOCK WRAPPER pass (E1-S2): how the resolver resolves
// a `block` item — a wrapper, DISTINCT from `container`, that wraps EXACTLY ONE
// inner content item and carries the cross-cutting, per-block concerns (a
// required stable `id`, an optional `theme` override, a human `title`/`label`,
// and a `visibility` flag) that apply uniformly to whatever it wraps.
//
// A block's single inner item lives in its `content` config field (a property of
// the block item-type schema referencing the document's #/$defs/instance), NOT in
// the document `children` array — a block groups no grid and arranges nothing.
// The loader walks only `children`, so it never links the content's $ref; this
// pass does that linking and resolves the content as a SEPARATE node:
//
//   - the wrapper node carries its own concerns (its interpolated, schema-validated
//     config with `id`/`theme`/`title`/`visibility`) and its configurable surface,
//     exactly like any item; the resolved content is NOT mixed into it (the
//     `content` field is lifted out of the wrapper's resolved config), and
//   - the inner content leaf is resolved IDENTICALLY to how it would resolve
//     unwrapped — same scoped variable environment, same interpolation, same
//     config validation — and emitted as the wrapper's single child so the two are
//     distinct nodes in the resolved tree.
//
// Two wrapper invariants are guarded fail-fast, with the offending wrapper path
// named, as defense-in-depth over the item-type schema:
//
//   - WRAPPER_ID_MISSING          — the wrapper's `id` is absent or whitespace-only
//                                    (the schema requires it with minLength 1; this
//                                    also catches a whitespace-only value).
//   - WRAPPER_CHILD_COUNT_INVALID — the wrapper does not wrap exactly one content
//                                    item (its `content` is absent/null or is not a
//                                    single instance object).
//
// The resolver stays "dumb" here: a block's `theme` override is VALIDATED against
// the shared theme vocabulary (E2-S3 — the block schema $refs the theme schema, so
// the wrapper's config-validation pass rejects an out-of-vocabulary token/value as
// RESOLVE_CONFIG_INVALID, path named) and then attached VERBATIM to the wrapper
// node. NO theme merge/cascade happens: the document default theme (E2-S2) and each
// wrapper override are emitted SIDE-BY-SIDE; composing the cascade is a downstream
// consumer's job. There is no effective/computed theme. Grammar/placement
// rules (root→regions, the wrapper-in-wrapper prohibition) are E3-S2 — this pass
// resolves only the wrapper's own concerns plus the two validations above.

import (
	"encoding/json"

	"github.com/frankbardon/lattice/errors"
	"github.com/frankbardon/lattice/internal/schema"
	"github.com/frankbardon/lattice/internal/variables"
)

// blockIDKey is the reserved config key carrying a wrapper's required stable
// anchor `id` (a config field of the wrapper item-type schema, not a document-level
// instance field). The single inner item's config key is NOT hardcoded: it is the
// wrapper schema's declared `latticeBehavior.contentField` (E4-S1), read via
// ResolvedType.ContentField() so a CUSTOM wrapper with a different field name
// resolves through this same pass. (The built-in `block` declares `content`.)
const blockIDKey = "id"

// resolveBlock resolves a block wrapper node and its single inner content as two
// SEPARATE resolved nodes. It runs the wrapper through the same per-node pipeline
// a leaf gets — scope extension, config interpolation, config validation — but
// only over the wrapper's OWN concerns (the `content` field is lifted out first),
// then resolves the inner content independently via the generic instance walk and
// emits it as the wrapper's single child. The wrapper's own invariants (required
// `id`, exactly-one content) are enforced fail-fast before any of this work, on
// the raw authored config. The `theme` override rides through the wrapper's own
// config validation (validated against the theme vocabulary, E2-S3) and is attached
// VERBATIM — NO cascade/merge here; the default and override are side-by-side.
func (r *Resolver) resolveBlock(g *schema.ResolvedGraph, inst *schema.Instance, rt *schema.ResolvedType, path string, parentEnv variables.Environment, raw *rawInstance, overrides variables.Overrides) (*ResolvedInstance, error) {
	// A block must declare a config (it carries its required id + content there). A
	// config-less block is missing both invariants; surface the id one first to
	// match the field order the schema checks.
	if inst.Config == nil {
		return nil, errors.NewCodedErrorWithDetails(errors.WRAPPER_ID_MISSING,
			"block wrapper is missing its required stable id",
			map[string]any{"path": path})
	}

	// The wrapper's single inner item lives in its schema-declared content field
	// (E4-S1) — `content` for the built-in block, any property name for a custom
	// wrapper. Index-time validation guarantees a wrapper carries a non-empty
	// contentField naming a real config property, so this is never empty here.
	contentField := rt.ContentField()

	// Enforce the wrapper's own-shape invariants and split its config into the inner
	// content instance and the wrapper's own concerns, BEFORE any interpolation, so
	// a malformed wrapper fails fast on the authored document.
	inner, ownConfig, err := extractBlockContent(inst.Config, contentField, path)
	if err != nil {
		return nil, err
	}

	// E3-S1: the wrapper layers its own variable declarations onto the inherited
	// environment, exactly like a leaf. The inner content inherits THIS environment
	// so it resolves identically to how it would unwrapped in the same position.
	var decls []variables.Declaration
	if raw != nil {
		decls = raw.Variables
	}
	env, err := parentEnv.ExtendWithOverrides(decls, path, overrides)
	if err != nil {
		return nil, err
	}

	// E3-S2: interpolate the wrapper's OWN concerns (title/visibility/theme/id) but
	// NOT the wrapped content — the content is interpolated on its own resolution
	// pass with its own scope. Interpolate preserves the top-level object shape.
	interpolatedOwn := ownConfig
	if len(ownConfig) > 0 {
		out, err := variables.Interpolate(ownConfig, env, path)
		if err != nil {
			return nil, err
		}
		interpolatedOwn, _ = out.(map[string]any)
	}

	// Validate the wrapper against its item-type schema. The block schema requires
	// `content`, so validation runs over the wrapper's interpolated own concerns
	// WITH the raw content re-attached (the content validates structurally against
	// #/$defs/instance; its own item-type validation happens on its separate pass).
	fullConfig := make(map[string]any, len(interpolatedOwn)+1)
	for k, v := range interpolatedOwn {
		fullConfig[k] = v
	}
	fullConfig[contentField] = inst.Config[contentField]
	if err := r.validateConfig(rt, fullConfig, path); err != nil {
		return nil, err
	}

	// The resolved wrapper node carries ONLY its own concerns: the content is lifted
	// out (it becomes a separate child node), so a consumer reads the inner item
	// once, as a node, never duplicated inside the wrapper's config.
	node := &ResolvedInstance{
		ID: inst.ID,
		Type: ResolvedTypeRef{
			Ref:     inst.Ref,
			ID:      rt.ID,
			Name:    rt.Name,
			Version: rt.Version,
		},
		Config:    interpolatedOwn,
		Placement: inst.Placement,
		VarEnv:    env,
	}

	// Link the inner content's type into the graph (the loader skipped it — it lives
	// in config, not children) and resolve it as a SEPARATE node using the generic
	// instance walk, so it resolves IDENTICALLY to an unwrapped instance: same env,
	// same interpolation, same config validation.
	if err := r.linkInnerType(g, inner); err != nil {
		return nil, err
	}
	innerRaw, err := rawInstanceFromContent(inst.Config[contentField].(map[string]any), path)
	if err != nil {
		return nil, err
	}
	innerPath := path + "." + contentField
	resolvedInner, err := r.resolveInstance(g, inner, innerPath, env, innerRaw, overrides)
	if err != nil {
		return nil, err
	}
	node.Children = []*ResolvedInstance{resolvedInner}

	return node, nil
}

// extractBlockContent pulls a block wrapper's single inner content instance out of
// its (raw, pre-interpolation) config, enforcing the wrapper's two own-shape
// invariants fail-fast: the wrapper must carry a stable `id` and must wrap exactly
// one content item. It returns the parsed inner instance plus a copy of the
// wrapper config with `content` removed (the wrapper's own concerns, to be
// interpolated and validated on their own — the content is resolved separately so
// it stays pure and is not duplicated on the wrapper node).
func extractBlockContent(config map[string]any, contentField, path string) (inner *schema.Instance, ownConfig map[string]any, err error) {
	// Defense-in-depth over the schema's required `id` (minLength 1): also reject a
	// present-but-whitespace id, which carries no stable anchor.
	id, _ := config[blockIDKey].(string)
	if isBlank(id) {
		return nil, nil, errors.NewCodedErrorWithDetails(errors.WRAPPER_ID_MISSING,
			"block wrapper is missing its required stable id",
			map[string]any{"path": path})
	}

	// A block wraps EXACTLY ONE content item. The schema models the content field as
	// a single instance object, so a structurally-valid block reaches here with one
	// object; the guard catches an absent/null content (zero) and any non-object
	// shape (e.g. an array, which would mean !=1) fail-fast.
	raw, present := config[contentField]
	if !present || raw == nil {
		return nil, nil, errors.NewCodedErrorWithDetails(errors.WRAPPER_CHILD_COUNT_INVALID,
			"block wrapper must wrap exactly one content item, but declares none",
			map[string]any{"path": path, "count": 0})
	}
	contentObj, ok := raw.(map[string]any)
	if !ok {
		// Anything that is not a single instance object (e.g. an array of items)
		// violates the exactly-one rule.
		return nil, nil, errors.NewCodedErrorWithDetails(errors.WRAPPER_CHILD_COUNT_INVALID,
			"block wrapper content is not a single content item",
			map[string]any{"path": path, "count": contentCount(raw)})
	}

	innerInst, err := decodeInstance(contentObj, path)
	if err != nil {
		return nil, nil, err
	}

	// Build the wrapper's own config view (every concern except the wrapped
	// content), so the wrapper's `id`/`theme`/`title`/`visibility` are interpolated
	// and validated without touching the inner item.
	ownConfig = make(map[string]any, len(config))
	for k, v := range config {
		if k == contentField {
			continue
		}
		ownConfig[k] = v
	}
	return innerInst, ownConfig, nil
}

// decodeInstance re-decodes a block's raw `content` object into a schema.Instance
// so the inner item can be resolved by exactly the same machinery as a document
// child. The content already validated against #/$defs/instance during the block
// config pass, so a decode failure here is an internal inconsistency.
func decodeInstance(content map[string]any, path string) (*schema.Instance, error) {
	data, err := json.Marshal(content)
	if err != nil {
		return nil, errors.WrapCodedErrorWithDetails(err, errors.RESOLVE_INTERNAL,
			"failed re-encoding block content for resolution",
			map[string]any{"path": path})
	}
	var inst schema.Instance
	if err := json.Unmarshal(data, &inst); err != nil {
		return nil, errors.WrapCodedErrorWithDetails(err, errors.RESOLVE_INTERNAL,
			"failed decoding block content instance",
			map[string]any{"path": path})
	}
	return &inst, nil
}

// rawInstanceFromContent builds the minimal generic variable view of a block's
// inner content (its own variable declarations and nested children), so the
// content resolves with the SAME variable plumbing an unwrapped instance gets.
// The wrapper carries no variable scope of its own beyond what it inherits, so
// the inner sees exactly the environment it would see if authored in place.
func rawInstanceFromContent(content map[string]any, path string) (*rawInstance, error) {
	data, err := json.Marshal(content)
	if err != nil {
		return nil, errors.WrapCodedErrorWithDetails(err, errors.RESOLVE_INTERNAL,
			"failed re-encoding block content for variable resolution",
			map[string]any{"path": path})
	}
	var raw rawInstance
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, errors.WrapCodedErrorWithDetails(err, errors.RESOLVE_INTERNAL,
			"failed decoding block content for variable resolution",
			map[string]any{"path": path})
	}
	return &raw, nil
}

// linkInnerType resolves the inner content's $ref through the loader and registers
// it in the graph (Types + Refs) so the generic instance walk can look up the
// inner item's resolved type. The loader's recursive walk skipped the content
// (it lives in config, not children), so this is where the content's type joins
// the graph. The inner content may itself be a children-bearing region/form (e.g.
// a block wrapping a form whose widget children live under it), so this recurses
// into the inner instance's `children` exactly like the loader's walk does — every
// descendant's type must be linked before the generic instance walk reaches it,
// or that descendant resolves with no type (RESOLVE_INTERNAL).
func (r *Resolver) linkInnerType(g *schema.ResolvedGraph, inner *schema.Instance) error {
	rt, err := r.loader.ResolveRef(g, inner.Ref)
	if err != nil {
		return err
	}
	g.Types[rt.ID] = rt
	g.Refs[inner] = rt.ID
	for _, child := range inner.Children {
		if err := r.linkInnerType(g, child); err != nil {
			return err
		}
	}
	return nil
}

// contentCount reports how many content items a non-object `content` value
// represents, for the WRAPPER_CHILD_COUNT_INVALID detail. An array reports its
// length; any other non-object scalar reports 1 (a malformed single item).
func contentCount(raw any) int {
	if arr, ok := raw.([]any); ok {
		return len(arr)
	}
	return 1
}

// isBlank reports whether s is empty or contains only whitespace.
func isBlank(s string) bool {
	for _, r := range s {
		if r != ' ' && r != '\t' && r != '\n' && r != '\r' {
			return false
		}
	}
	return true
}
