package schema

import "github.com/frankbardon/lattice/errors"

// This file owns the `latticeBehavior` schema-level keyword: the single,
// keyword-driven vocabulary that tells the resolver how an item type
// participates in the tree (its ROLE) and the role-specific knobs that govern
// that participation. It generalizes the older single-purpose `positional`
// marker (see catalog.go): a positional region is now expressed as
// `latticeBehavior.role == "region"`.
//
// Like `positional`, `latticeBehavior` is a top-level, schema-level keyword (a
// sibling of `properties`, NOT per-instance config). google/jsonschema-go
// captures it as an unknown keyword in Schema.Extra, so reading it is O(1) at
// index time with no extra schema walk. This file is the SINGLE SOURCE OF
// TRUTH for decoding that keyword; the resolver dispatch passes (E2–E4) consume
// the typed accessors here instead of hardcoding type-name lists.
//
// SCOPE NOTE (E1-S1): this story only parses the keyword and exposes typed
// accessors. It performs NO index-time validation of role/subkey combinations
// (that is E1-S2, via SCHEMA_BEHAVIOR_INVALID) and changes NO resolver
// dispatch — built-in resolution stays name-keyed until later epics. Accessors
// therefore read defensively and report zero values for absent or malformed
// keywords rather than erroring.

// behaviorKey is the reserved schema-level keyword carrying the role vocabulary.
// It is captured into Schema.Extra by the loader exactly like positionalKey.
const behaviorKey = "latticeBehavior"

// Role names the three ways an item type participates in the resolved tree. A
// type that declares no `latticeBehavior` (or an unrecognized role) is a
// "plain leaf" and reports the empty RoleNone.
type Role string

const (
	// RoleNone is the zero value: the type declares no latticeBehavior role and
	// is treated as a plain leaf.
	RoleNone Role = ""

	// RoleRegion is a layout-only container that positions children and carries
	// no chrome of its own. This subsumes the legacy `positional: true` marker.
	RoleRegion Role = "region"

	// RoleWrapper wraps a single inner instance, lifting it from a named config
	// field (see ContentField).
	RoleWrapper Role = "wrapper"

	// RoleWidget is a leaf that binds to a variable/value (see Binds).
	RoleWidget Role = "widget"
)

// ChildPolicy constrains what kinds of children a region may hold. It is only
// meaningful for RoleRegion types; other roles report the empty value.
type ChildPolicy string

const (
	// ChildPolicyNone is the zero value (no policy declared).
	ChildPolicyNone ChildPolicy = ""

	// ChildPolicyRegionsOrWrappers permits region and wrapper children only.
	ChildPolicyRegionsOrWrappers ChildPolicy = "regions-or-wrappers"

	// ChildPolicyWidgets permits widget children only.
	ChildPolicyWidgets ChildPolicy = "widgets"
)

// Layout selects how a region arranges its children. Only meaningful for
// RoleRegion types; other roles report the empty value.
type Layout string

const (
	// LayoutNone is the explicitly declared "no managed layout" arrangement.
	// `none` (not `stack`) is the canonical keyword for an unmanaged/stacked
	// region, matching the vocabulary in the interview. NOTE: this is the
	// EXPLICIT value; a region that declares no `layout` at all reads as the
	// empty Layout zero value, which the resolver may treat as none in E3.
	LayoutNone Layout = "none"

	// LayoutGrid arranges children on a positional grid.
	LayoutGrid Layout = "grid"

	// LayoutFlow arranges children in document flow (e.g. form fields).
	LayoutFlow Layout = "flow"
)

// behavior is the decoded view of the `latticeBehavior` keyword for one
// resolved type. It is computed on demand from Schema.Extra and is never
// persisted; the accessors below are the public surface.
type behavior struct {
	role           Role
	childPolicy    ChildPolicy
	layout         Layout
	contentField   string
	binds          []string
	requireOptions bool
	rangeCheck     bool
}

// behavior decodes the `latticeBehavior` keyword from the resolved type's
// preserved Schema.Extra. It reads defensively: any missing, mistyped, or
// absent field yields that field's zero value, and an absent keyword yields a
// zero-value behavior whose role is RoleNone (a plain leaf). Validation of
// role/subkey coherence is deferred to E1-S2.
func (rt *ResolvedType) behavior() behavior {
	var b behavior
	if rt == nil || rt.Schema == nil || rt.Schema.Extra == nil {
		return b
	}
	raw, ok := rt.Schema.Extra[behaviorKey].(map[string]any)
	if !ok {
		return b
	}
	if v, ok := raw["role"].(string); ok {
		b.role = Role(v)
	}
	if v, ok := raw["childPolicy"].(string); ok {
		b.childPolicy = ChildPolicy(v)
	}
	if v, ok := raw["layout"].(string); ok {
		b.layout = Layout(v)
	}
	if v, ok := raw["contentField"].(string); ok {
		b.contentField = v
	}
	if v, ok := raw["binds"].([]any); ok {
		b.binds = make([]string, 0, len(v))
		for _, e := range v {
			if s, ok := e.(string); ok {
				b.binds = append(b.binds, s)
			}
		}
	}
	if v, ok := raw["requireOptions"].(bool); ok {
		b.requireOptions = v
	}
	if v, ok := raw["rangeCheck"].(bool); ok {
		b.rangeCheck = v
	}
	return b
}

// Role reports the item type's declared latticeBehavior role, or RoleNone if it
// declares no role (a plain leaf).
func (rt *ResolvedType) Role() Role { return rt.behavior().role }

// ChildPolicy reports a region's child-admission policy. Empty for non-regions
// or regions that declare none.
func (rt *ResolvedType) ChildPolicy() ChildPolicy { return rt.behavior().childPolicy }

// Layout reports a region's child-arrangement layout. Empty (LayoutNone) for
// non-regions or regions that declare none.
func (rt *ResolvedType) Layout() Layout { return rt.behavior().layout }

// ContentField reports the wrapper config field whose value holds the wrapped
// inner instance. Empty for non-wrappers or wrappers that declare none.
func (rt *ResolvedType) ContentField() string { return rt.behavior().contentField }

// Binds reports the value kinds a widget may bind to (e.g. "string", "number").
// Nil for non-widgets or widgets that declare none.
func (rt *ResolvedType) Binds() []string { return rt.behavior().binds }

// RequireOptions reports whether a widget requires an options set (enum-style).
// False for non-widgets or widgets that omit the flag.
func (rt *ResolvedType) RequireOptions() bool { return rt.behavior().requireOptions }

// RangeCheck reports whether a widget enforces a numeric range. False for
// non-widgets or widgets that omit the flag.
func (rt *ResolvedType) RangeCheck() bool { return rt.behavior().rangeCheck }

// bindKinds is the set of variable-value kinds a widget may legally bind to. A
// widget's `binds` list must be non-empty and every member must appear here. The
// kinds mirror the variable type vocabulary (string/number/integer/boolean/enum/
// array) — the permitted-type set that supersedes the former name-keyed family map.
var bindKinds = map[string]bool{
	"string":  true,
	"number":  true,
	"integer": true,
	"boolean": true,
	"enum":    true,
	"array":   true,
}

// validateBehavior validates the `latticeBehavior` keyword (if present) for one
// resolved item type at catalog-index time, failing fast with the offending
// schema named. It is the erroring counterpart to the defensive accessors above:
// where the accessors report zero values for malformed fields, this rejects them.
//
// A type that declares NO `latticeBehavior` block is a plain leaf and validates
// cleanly — the block is never required. When present it must be coherent:
//   - `role` must be present and name one of region/wrapper/widget;
//   - a `wrapper` requires a `contentField` that names a declared config property;
//   - a `widget` requires a non-empty `binds`, each member a known bind kind;
//   - `childPolicy` and `layout` are region-only, and when present must name a
//     known enum value;
//   - every typed field must carry its expected JSON type.
//
// Validation is O(1) per type (a single Schema.Extra read, no schema walk), so it
// adds no per-document cost — it runs once when the ResolvedType is indexed.
func (rt *ResolvedType) validateBehavior() error {
	if rt == nil || rt.Schema == nil || rt.Schema.Extra == nil {
		return nil
	}
	rawAny, present := rt.Schema.Extra[behaviorKey]
	if !present {
		return nil // plain leaf / legacy positional — nothing to validate.
	}
	raw, ok := rawAny.(map[string]any)
	if !ok {
		return rt.behaviorErr("latticeBehavior must be an object", nil)
	}

	// role — required, must be a known enum value.
	roleAny, hasRole := raw["role"]
	if !hasRole {
		return rt.behaviorErr("latticeBehavior is missing required field \"role\"",
			map[string]any{"field": "role"})
	}
	roleStr, ok := roleAny.(string)
	if !ok {
		return rt.behaviorErr("latticeBehavior \"role\" must be a string",
			map[string]any{"field": "role", "value": roleAny})
	}
	role := Role(roleStr)
	switch role {
	case RoleRegion, RoleWrapper, RoleWidget:
	default:
		return rt.behaviorErr("latticeBehavior \"role\" names an unknown role (want region, wrapper, or widget)",
			map[string]any{"field": "role", "value": roleStr})
	}

	// childPolicy — region-only; when present must be a known enum value.
	if cpAny, has := raw["childPolicy"]; has {
		if role != RoleRegion {
			return rt.behaviorErr("latticeBehavior \"childPolicy\" is only valid on a region role",
				map[string]any{"field": "childPolicy", "value": cpAny})
		}
		cp, ok := cpAny.(string)
		if !ok {
			return rt.behaviorErr("latticeBehavior \"childPolicy\" must be a string",
				map[string]any{"field": "childPolicy", "value": cpAny})
		}
		switch ChildPolicy(cp) {
		case ChildPolicyRegionsOrWrappers, ChildPolicyWidgets:
		default:
			return rt.behaviorErr("latticeBehavior \"childPolicy\" names an unknown policy (want regions-or-wrappers or widgets)",
				map[string]any{"field": "childPolicy", "value": cp})
		}
	}

	// layout — region-only; when present must be a known enum value.
	if loAny, has := raw["layout"]; has {
		if role != RoleRegion {
			return rt.behaviorErr("latticeBehavior \"layout\" is only valid on a region role",
				map[string]any{"field": "layout", "value": loAny})
		}
		lo, ok := loAny.(string)
		if !ok {
			return rt.behaviorErr("latticeBehavior \"layout\" must be a string",
				map[string]any{"field": "layout", "value": loAny})
		}
		switch Layout(lo) {
		case LayoutNone, LayoutGrid, LayoutFlow:
		default:
			return rt.behaviorErr("latticeBehavior \"layout\" names an unknown layout (want none, grid, or flow)",
				map[string]any{"field": "layout", "value": lo})
		}
	}

	// contentField — required on a wrapper; when present must be a string naming a
	// declared config property. It is wrapper-only knowledge but harmless on other
	// roles, so only its required-ness on a wrapper is enforced here.
	if cfAny, has := raw["contentField"]; has {
		cf, ok := cfAny.(string)
		if !ok {
			return rt.behaviorErr("latticeBehavior \"contentField\" must be a string",
				map[string]any{"field": "contentField", "value": cfAny})
		}
		if cf != "" && !rt.hasProperty(cf) {
			return rt.behaviorErr("latticeBehavior \"contentField\" names a config property the schema does not declare",
				map[string]any{"field": "contentField", "value": cf})
		}
	}
	if role == RoleWrapper {
		cf, _ := raw["contentField"].(string)
		if cf == "" {
			return rt.behaviorErr("latticeBehavior wrapper role requires a non-empty \"contentField\"",
				map[string]any{"field": "contentField"})
		}
	}

	// binds — required and non-empty on a widget; every member a known bind kind.
	if bAny, has := raw["binds"]; has {
		arr, ok := bAny.([]any)
		if !ok {
			return rt.behaviorErr("latticeBehavior \"binds\" must be an array of strings",
				map[string]any{"field": "binds", "value": bAny})
		}
		for _, e := range arr {
			s, ok := e.(string)
			if !ok {
				return rt.behaviorErr("latticeBehavior \"binds\" member must be a string",
					map[string]any{"field": "binds", "value": e})
			}
			if !bindKinds[s] {
				return rt.behaviorErr("latticeBehavior \"binds\" names an unknown bind kind (want string, number, integer, boolean, enum, or array)",
					map[string]any{"field": "binds", "value": s})
			}
		}
	}
	if role == RoleWidget {
		arr, _ := raw["binds"].([]any)
		if len(arr) == 0 {
			return rt.behaviorErr("latticeBehavior widget role requires a non-empty \"binds\"",
				map[string]any{"field": "binds"})
		}
	}

	return nil
}

// hasProperty reports whether the resolved item-type schema declares a top-level
// config property by the given name.
func (rt *ResolvedType) hasProperty(name string) bool {
	if rt.Schema == nil || rt.Schema.Properties == nil {
		return false
	}
	_, ok := rt.Schema.Properties[name]
	return ok
}

// behaviorErr builds a SCHEMA_BEHAVIOR_INVALID coded error naming the offending
// schema in Details["schema"] (its $id, falling back to its source path), merging
// any extra field/value details.
func (rt *ResolvedType) behaviorErr(message string, extra map[string]any) error {
	details := map[string]any{"schema": rt.schemaName()}
	for k, v := range extra {
		details[k] = v
	}
	return errors.NewCodedErrorWithDetails(errors.SCHEMA_BEHAVIOR_INVALID, message, details)
}

// schemaName returns the most identifying name for the schema in diagnostics: its
// canonical $id, falling back to its source path, then its parsed name.
func (rt *ResolvedType) schemaName() string {
	switch {
	case rt.ID != "":
		return rt.ID
	case rt.Source != "":
		return rt.Source
	default:
		return rt.Name
	}
}
