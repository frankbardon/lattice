package changeset

// This file owns the APPLY ENGINE and the FIELD-EDIT GUARDRAIL (E1-S2): the step
// that turns a parsed, id-rooted changeset into mutated, canonically-serialized
// document bytes, after proving every field-level edit targets a field the
// configurable surface actually exposes.
//
// THE PIPELINE. The document on disk is loaded fully-decoded (a patch cannot
// apply to raw bytes), so the apply path is: stored bytes -> decode to a generic
// JSON tree -> enforce the field-edit guardrail against the RESOLVED current
// document's surfaces -> translate each id-rooted pointer to a physical RFC 6901
// pointer (translate.go) -> apply the RFC 6902 operations with the standard
// applier (github.com/evanphx/json-patch/v5) -> CANONICALLY re-marshal the
// mutated tree (sorted keys, fixed 2-space indent). Any failure rejects the whole
// changeset and returns nothing applied — the caller persists only a successful
// result (atomicity is the caller's Save-or-not decision; this function is pure).
//
// THE FIELD-EDIT GUARDRAIL. A field-level edit may only touch a field the
// relevant configurable SURFACE exposes — an item node's surface (resolved per
// node, keyed by item id) for an `<id>`-rooted edit, or a document scope's surface
// (`$manifest` title/description, `$theme` tokens) for a `$`-rooted edit. The
// surfaces are the resolver-computed ConfigurableField sets carried on the
// resolved tree (each node's Surface; ResolvedTree.ScopeSurfaces for the scopes),
// so this guardrail can never drift from what the resolver considers settable. An
// edit whose target is NOT on the surface — an unsurfaced field, or a NESTED path
// (surfaces cover TOP-LEVEL fields only; nested editing is a later epic) — is
// rejected with CONFIG_OVERRIDE_FIELD_UNKNOWN, mirroring the resolver's
// config-override pass. The edit's value is coerced/validated against the surface
// field's declared type exactly as the config-override pass does, rejected with
// CONFIG_OVERRIDE_VALUE_INVALID. Re-resolving the MUTATED document (a later story)
// is the structural/constraint guardrail; this story enforces only the
// surface-membership + value-type contract for field edits.

import (
	"encoding/json"
	"strings"

	jsonpatch "github.com/evanphx/json-patch/v5"

	"github.com/frankbardon/lattice/errors"
	"github.com/frankbardon/lattice/internal/resolver"
	"github.com/frankbardon/lattice/internal/variables"
)

// canonicalIndent is the fixed indentation a canonically re-marshaled document
// uses: two spaces per level. Combined with encoding/json's sorted map-key
// emission, it makes the serialized form DETERMINISTIC — an already-canonical
// document re-marshaled after a no-op changeset round-trips to identical bytes.
const canonicalIndent = "  "

// configKey is the instance member under which an item's configurable fields
// live. A field-level edit of an item targets `/<id>/config/<field>`, so the
// guardrail reads the surface field name from the path segment AFTER `config`.
const configKey = "config"

// applyToBytes applies the parsed, id-rooted changeset cs to the decoded document
// and returns the mutated document CANONICALLY re-marshaled (sorted keys, fixed
// 2-space indent). docBytes is the stored document; tree is the RESOLVED current
// document (resolved read-only by the caller) whose configurable surfaces the
// field-edit guardrail is checked against — it must be the resolution of the SAME
// bytes, so the surfaces describe the document being patched.
//
// The steps are: decode the document, enforce the field-edit guardrail against the
// resolved surfaces (rejecting any edit off a configurable surface, or any nested
// path, with CONFIG_OVERRIDE_FIELD_UNKNOWN, and any ill-typed value with
// CONFIG_OVERRIDE_VALUE_INVALID), translate each id-rooted pointer to a physical
// RFC 6901 pointer, apply the RFC 6902 operations with the standard applier, then
// canonically re-marshal. It is FAIL-FAST and pure: the first violation aborts and
// nothing is applied; on any error the caller persists nothing.
//
// This is the pure APPLY ENGINE — it neither loads nor persists. The public
// ApplyChangeset (pipeline.go) owns the full load -> resolve -> apply -> re-resolve
// -> Save pipeline and wraps this step. Splitting them keeps the engine pure (and
// directly testable on bytes) while the pipeline owns the Store and the atomic
// reject-or-persist decision.
func applyToBytes(docBytes []byte, cs *Changeset, tree *resolver.ResolvedTree) ([]byte, error) {
	var doc map[string]any
	if err := json.Unmarshal(docBytes, &doc); err != nil {
		return nil, errors.WrapCodedError(err, errors.RESOLVE_DOCUMENT_INVALID,
			"document being patched is not a valid JSON object")
	}

	surfaces := newSurfaceIndex(tree)

	// Field-edit guardrail: every op's target field must be on the relevant
	// configurable surface, and its value must satisfy that field's declared type.
	// Checked on the ID-ROOTED ops (before translation) so the leading id/scope and
	// the target field are read directly from the authored pointer.
	for i, op := range cs.Ops {
		if err := surfaces.checkOp(op, i); err != nil {
			return nil, err
		}
	}

	// Translate id-rooted pointers to physical RFC 6901, then apply with the
	// standard RFC 6902 applier.
	translator := NewTranslator(doc)
	physical, err := translator.TranslateChangeset(cs)
	if err != nil {
		return nil, err
	}
	mutated, err := applyPhysical(docBytes, physical)
	if err != nil {
		return nil, err
	}

	// Canonical re-marshal: decode the applier's output and re-serialize it
	// deterministically.
	return canonicalize(mutated)
}

// applyPhysical applies a physical (already-translated) RFC 6902 changeset to the
// document bytes via github.com/evanphx/json-patch/v5. It serializes the ops to a
// JSON Patch array, decodes it through the applier, and applies it; the applier
// owns array-index/`-`/escaping and atomic-rollback semantics. A malformed patch
// or a failed operation (e.g. a `test` mismatch, a remove of a missing member)
// surfaces as PATCH_APPLY_FAILED.
func applyPhysical(docBytes []byte, physical *Changeset) ([]byte, error) {
	patchJSON, err := marshalPatch(physical)
	if err != nil {
		return nil, err
	}
	patch, err := jsonpatch.DecodePatch(patchJSON)
	if err != nil {
		return nil, errors.WrapCodedError(err, errors.PATCH_APPLY_FAILED,
			"translated changeset is not a decodable RFC 6902 patch")
	}
	out, err := patch.Apply(docBytes)
	if err != nil {
		return nil, errors.WrapCodedError(err, errors.PATCH_APPLY_FAILED,
			"changeset could not be applied to the document")
	}
	return out, nil
}

// marshalPatch serializes a physical changeset to a standard RFC 6902 JSON Patch
// array — exactly the dialect github.com/evanphx/json-patch/v5 consumes. Each
// operation emits its `op` and `path`, plus a verbatim `value` (for add/replace/
// test) and a `from` (for move/copy) when present.
func marshalPatch(cs *Changeset) ([]byte, error) {
	type wireOp struct {
		Op    string          `json:"op"`
		Path  string          `json:"path"`
		From  string          `json:"from,omitempty"`
		Value json.RawMessage `json:"value,omitempty"`
	}
	wire := make([]wireOp, 0, len(cs.Ops))
	for _, op := range cs.Ops {
		w := wireOp{Op: op.Op, Path: op.Path}
		if op.HasFrom {
			w.From = op.From
		}
		if op.HasValue {
			w.Value = op.Value
		}
		wire = append(wire, w)
	}
	out, err := json.Marshal(wire)
	if err != nil {
		return nil, errors.WrapCodedError(err, errors.PATCH_APPLY_FAILED,
			"failed serializing the translated changeset for application")
	}
	return out, nil
}

// canonicalize decodes mutated document bytes and re-serializes them
// DETERMINISTICALLY: encoding/json emits object keys in sorted order, and the
// fixed 2-space indent fixes the layout, so the same logical document always
// produces identical bytes (an already-canonical document round-trips unchanged).
func canonicalize(mutated []byte) ([]byte, error) {
	var tree any
	if err := json.Unmarshal(mutated, &tree); err != nil {
		return nil, errors.WrapCodedError(err, errors.RESOLVE_INTERNAL,
			"mutated document is not valid JSON after applying the changeset")
	}
	out, err := json.MarshalIndent(tree, "", canonicalIndent)
	if err != nil {
		return nil, errors.WrapCodedError(err, errors.RESOLVE_INTERNAL,
			"failed canonically serializing the mutated document")
	}
	return out, nil
}

// surfaceIndex resolves an id-rooted changeset op to the configurable surface that
// governs its target and the field it edits. Item surfaces are indexed by item id
// (walked from the resolved tree); document-scope surfaces are looked up by their
// reserved `$`-keyword. It is the guardrail's view of "what is settable."
type surfaceIndex struct {
	// items maps an item id to that node's configurable surface (resolver-computed).
	items map[string][]resolver.ConfigurableField
	// scopes maps a reserved `$`-keyword to that scope's surface (resolver-computed).
	scopes map[string][]resolver.ConfigurableField
}

// newSurfaceIndex builds the guardrail's surface view from a resolved tree: it
// walks the tree to index every id-carrying node's Surface and carries the
// document-scope surfaces verbatim. A nil tree yields an empty index (every edit
// is then off-surface and rejected).
func newSurfaceIndex(tree *resolver.ResolvedTree) *surfaceIndex {
	idx := &surfaceIndex{
		items:  map[string][]resolver.ConfigurableField{},
		scopes: map[string][]resolver.ConfigurableField{},
	}
	if tree == nil {
		return idx
	}
	idx.indexInstance(tree.Root)
	for scope, surface := range tree.ScopeSurfaces {
		idx.scopes[scope] = surface
	}
	return idx
}

// indexInstance records inst's surface (if it carries an id) and recurses into its
// resolved children. A block wrapper's inner content is a child in the resolved
// tree, so the recursion reaches every id-carrying node — exactly the set the
// id-rooted pointer can address.
func (s *surfaceIndex) indexInstance(inst *resolver.ResolvedInstance) {
	if inst == nil {
		return
	}
	if inst.ID != "" {
		s.items[inst.ID] = inst.Surface
	}
	for _, child := range inst.Children {
		s.indexInstance(child)
	}
}

// checkOp enforces the field-edit guardrail for one id-rooted op: it resolves the
// op's target field (the leading id/scope plus the single field segment), requires
// that field to be on the relevant configurable surface, and validates the op's
// value (for add/replace/test) against the surface field's declared type. opIndex
// is carried into error Details. move/copy `from` is not a value write, so only
// the `path` target is guarded here.
func (s *surfaceIndex) checkOp(op Operation, opIndex int) error {
	surface, field, err := s.resolveTarget(op.Path, opIndex)
	if err != nil {
		return err
	}

	sf, ok := surfaceField(surface, field)
	if !ok {
		return errors.NewCodedErrorWithDetails(errors.CONFIG_OVERRIDE_FIELD_UNKNOWN,
			"changeset edit targets a field not on the target's configurable surface",
			map[string]any{"pointer": op.Path, "field": field, "index": opIndex})
	}

	// Only value-carrying ops write a value to coerce. A remove targeting a
	// surfaced field is permitted by the surface check alone.
	if op.HasValue {
		if err := coerceFieldValue(op.Value, sf, op.Path, field, opIndex); err != nil {
			return err
		}
	}
	return nil
}

// resolveTarget parses an id-rooted op pointer into the configurable surface that
// governs it and the single TOP-LEVEL field it edits. A `$`-scope pointer is
// `/$scope/<field>` (the scope surface, e.g. `$manifest`/`$theme`); an item
// pointer is `/<id>/config/<field>` (the item node's surface). Anything else — a
// pointer that addresses the node/scope as a whole, an item path not rooted at
// `config`, or a NESTED path with extra segments past the field — is rejected as
// off-surface (CONFIG_OVERRIDE_FIELD_UNKNOWN): surfaces cover top-level fields
// only, and nested editing is a later epic.
func (s *surfaceIndex) resolveTarget(pointer string, opIndex int) ([]resolver.ConfigurableField, string, error) {
	if pointer == "" || pointer[0] != '/' {
		return nil, "", errors.NewCodedErrorWithDetails(errors.CHANGESET_POINTER_INVALID,
			"changeset pointer is empty or not rooted at \"/\"",
			map[string]any{"pointer": pointer, "index": opIndex})
	}

	leading, remainder, _ := strings.Cut(pointer[1:], "/")
	if leading == "" {
		return nil, "", errors.NewCodedErrorWithDetails(errors.CHANGESET_POINTER_INVALID,
			"changeset pointer has an empty leading id/scope segment",
			map[string]any{"pointer": pointer, "index": opIndex})
	}

	if strings.HasPrefix(leading, reservedScopePrefix) {
		return s.resolveScopeTarget(leading, remainder, pointer, opIndex)
	}
	return s.resolveItemTarget(leading, remainder, pointer, opIndex)
}

// resolveScopeTarget resolves a `$`-scope op pointer to its scope surface and the
// edited field. A scope surface exists only for the settable scopes (`$manifest`/
// `$theme`); a scope with no surface, an unknown `$`-keyword, or a non-top-level
// remainder (empty, or nested past the field) is off-surface.
func (s *surfaceIndex) resolveScopeTarget(scope, remainder, pointer string, opIndex int) ([]resolver.ConfigurableField, string, error) {
	field, ok := topLevelField(remainder)
	if !ok {
		return nil, "", offSurface(pointer, opIndex)
	}
	surface, ok := s.scopes[scope]
	if !ok {
		return nil, "", offSurface(pointer, opIndex)
	}
	return surface, field, nil
}

// resolveItemTarget resolves an `<id>`-rooted op pointer to the item node's
// surface and the edited config field. The remainder must be exactly
// `config/<field>` (a top-level config field); anything else — a path not under
// `config`, the node as a whole, or a nested path past the field — is off-surface.
// An id naming no node in the resolved tree is likewise off-surface (there is no
// surface that exposes it), mirroring the config-override pass.
func (s *surfaceIndex) resolveItemTarget(id, remainder, pointer string, opIndex int) ([]resolver.ConfigurableField, string, error) {
	rest, ok := strings.CutPrefix(remainder, configKey+"/")
	if !ok {
		return nil, "", offSurface(pointer, opIndex)
	}
	field, ok := topLevelField(rest)
	if !ok {
		return nil, "", offSurface(pointer, opIndex)
	}
	surface, ok := s.items[id]
	if !ok {
		return nil, "", offSurface(pointer, opIndex)
	}
	return surface, field, nil
}

// topLevelField returns the single field segment of a pointer remainder and true
// when the remainder names exactly one segment (a top-level field). An empty
// remainder (addresses the container itself) or a multi-segment remainder (a
// nested path) returns false — surfaces cover top-level fields only.
func topLevelField(remainder string) (string, bool) {
	if remainder == "" || strings.Contains(remainder, "/") {
		return "", false
	}
	return remainder, true
}

// offSurface is the standard rejection for a changeset edit whose target is not a
// top-level configurable-surface field — an unsurfaced field, a nested path, or a
// target with no surface. It reuses the config-override pass's
// CONFIG_OVERRIDE_FIELD_UNKNOWN so a field-level guardrail violation reads the same
// whether it came from a runtime override or a persisted changeset.
func offSurface(pointer string, opIndex int) error {
	return errors.NewCodedErrorWithDetails(errors.CONFIG_OVERRIDE_FIELD_UNKNOWN,
		"changeset edit targets a path that is not a top-level configurable-surface field",
		map[string]any{"pointer": pointer, "index": opIndex})
}

// surfaceField returns the configurable-surface entry for field and whether the
// surface exposes it. It mirrors the resolver's config-override lookup so the
// changeset guardrail and the runtime-override guardrail agree on membership.
func surfaceField(surface []resolver.ConfigurableField, field string) (resolver.ConfigurableField, bool) {
	for _, f := range surface {
		if f.Field == field {
			return f, true
		}
	}
	return resolver.ConfigurableField{}, false
}

// coerceFieldValue validates an op's raw JSON value against a surface field's
// declared type (and enum options), reusing the same variables.CoerceValue the
// config-override pass uses so a changeset edit and a runtime override accept the
// identical value set. A wrong-typed or out-of-option value is rejected with
// CONFIG_OVERRIDE_VALUE_INVALID. The value is decoded from its verbatim raw JSON
// to a generic Go value before coercion.
func coerceFieldValue(raw json.RawMessage, sf resolver.ConfigurableField, pointer, field string, opIndex int) error {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return errors.WrapCodedErrorWithDetails(err, errors.CONFIG_OVERRIDE_VALUE_INVALID,
			"changeset edit value is not valid JSON",
			map[string]any{"pointer": pointer, "field": field, "index": opIndex})
	}
	if _, err := variables.CoerceValue(value, sf.Type, surfaceEnumOptions(sf), pointer, field); err != nil {
		return errors.WrapCodedErrorWithDetails(err, errors.CONFIG_OVERRIDE_VALUE_INVALID,
			"changeset edit value does not satisfy the surface field's declared type",
			map[string]any{"pointer": pointer, "field": field, "index": opIndex})
	}
	return nil
}

// surfaceEnumOptions extracts the enum option set for an enum-typed surface field
// from its opaque constraints (a JSON Schema-ish `enum` array), mirroring the
// resolver's config-override extraction. It returns nil for non-enum fields or when
// no options are declared, in which case the value is type-checked but not
// membership-checked.
func surfaceEnumOptions(f resolver.ConfigurableField) []any {
	if f.Type != variables.VarTypeEnum || f.Constraints == nil {
		return nil
	}
	opts, _ := f.Constraints["enum"].([]any)
	return opts
}
