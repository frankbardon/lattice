package schema

import (
	"reflect"
	"testing"
)

// resolvedFromJSON parses a single item-type schema JSON blob into a
// ResolvedType, exercising the same loader parse path the catalog uses (so the
// keyword lands in Schema.Extra exactly as in production).
func resolvedFromJSON(t *testing.T, raw string) *ResolvedType {
	t.Helper()
	sch, err := parseSchema([]byte(raw))
	if err != nil {
		t.Fatalf("parseSchema: %v", err)
	}
	return &ResolvedType{ID: sch.ID, Schema: sch}
}

func TestBehaviorRegionAccessors(t *testing.T) {
	rt := resolvedFromJSON(t, `{
		"$id": "https://lattice.dev/schemas/items/region-test/1.0.0",
		"type": "object",
		"latticeBehavior": {
			"role": "region",
			"childPolicy": "widgets",
			"layout": "grid"
		}
	}`)

	if got := rt.Role(); got != RoleRegion {
		t.Errorf("Role() = %q, want %q", got, RoleRegion)
	}
	if got := rt.ChildPolicy(); got != ChildPolicyWidgets {
		t.Errorf("ChildPolicy() = %q, want %q", got, ChildPolicyWidgets)
	}
	if got := rt.Layout(); got != LayoutGrid {
		t.Errorf("Layout() = %q, want %q", got, LayoutGrid)
	}
	// Region carries no wrapper/widget knobs.
	if got := rt.ContentField(); got != "" {
		t.Errorf("ContentField() = %q, want empty", got)
	}
	if rt.Binds() != nil {
		t.Errorf("Binds() = %v, want nil", rt.Binds())
	}

	// An explicitly declared layout:"none" reads as LayoutNone, distinct from
	// the empty zero value an undeclared layout produces.
	flow := resolvedFromJSON(t, `{
		"$id": "https://lattice.dev/schemas/items/region-flow/1.0.0",
		"type": "object",
		"latticeBehavior": { "role": "region", "childPolicy": "regions-or-wrappers", "layout": "none" }
	}`)
	if got := flow.Layout(); got != LayoutNone {
		t.Errorf("explicit layout Layout() = %q, want %q", got, LayoutNone)
	}
	if got := flow.ChildPolicy(); got != ChildPolicyRegionsOrWrappers {
		t.Errorf("ChildPolicy() = %q, want %q", got, ChildPolicyRegionsOrWrappers)
	}
}

func TestBehaviorWrapperAccessors(t *testing.T) {
	rt := resolvedFromJSON(t, `{
		"$id": "https://lattice.dev/schemas/items/wrapper-test/1.0.0",
		"type": "object",
		"latticeBehavior": {
			"role": "wrapper",
			"contentField": "content"
		}
	}`)

	if got := rt.Role(); got != RoleWrapper {
		t.Errorf("Role() = %q, want %q", got, RoleWrapper)
	}
	if got := rt.ContentField(); got != "content" {
		t.Errorf("ContentField() = %q, want %q", got, "content")
	}
	// Wrapper carries no region/widget knobs. An undeclared layout reads as the
	// zero value (empty), distinct from an explicitly declared LayoutNone.
	if got := rt.ChildPolicy(); got != ChildPolicyNone {
		t.Errorf("ChildPolicy() = %q, want empty", got)
	}
	if got := rt.Layout(); got != "" {
		t.Errorf("Layout() = %q, want empty", got)
	}
}

func TestBehaviorWidgetAccessors(t *testing.T) {
	rt := resolvedFromJSON(t, `{
		"$id": "https://lattice.dev/schemas/items/widget-test/1.0.0",
		"type": "object",
		"latticeBehavior": {
			"role": "widget",
			"binds": ["string", "number", "enum"],
			"requireOptions": true,
			"rangeCheck": true
		}
	}`)

	if got := rt.Role(); got != RoleWidget {
		t.Errorf("Role() = %q, want %q", got, RoleWidget)
	}
	wantBinds := []string{"string", "number", "enum"}
	if got := rt.Binds(); !reflect.DeepEqual(got, wantBinds) {
		t.Errorf("Binds() = %v, want %v", got, wantBinds)
	}
	if !rt.RequireOptions() {
		t.Error("RequireOptions() = false, want true")
	}
	if !rt.RangeCheck() {
		t.Error("RangeCheck() = false, want true")
	}
}

func TestBehaviorAbsentIsPlainLeaf(t *testing.T) {
	rt := resolvedFromJSON(t, `{
		"$id": "https://lattice.dev/schemas/items/leaf-test/1.0.0",
		"type": "object",
		"properties": { "title": { "type": "string" } }
	}`)

	if got := rt.Role(); got != RoleNone {
		t.Errorf("Role() = %q, want RoleNone (plain leaf)", got)
	}
	if got := rt.ChildPolicy(); got != ChildPolicyNone {
		t.Errorf("ChildPolicy() = %q, want empty", got)
	}
	if got := rt.Layout(); got != "" {
		t.Errorf("Layout() = %q, want empty (zero value, not explicit none)", got)
	}
	if got := rt.ContentField(); got != "" {
		t.Errorf("ContentField() = %q, want empty", got)
	}
	if rt.Binds() != nil {
		t.Errorf("Binds() = %v, want nil", rt.Binds())
	}
	if rt.RequireOptions() {
		t.Error("RequireOptions() = true, want false")
	}
	if rt.RangeCheck() {
		t.Error("RangeCheck() = true, want false")
	}
}

// Nil and schemaless receivers must never panic; they report zero values.
func TestBehaviorNilSafe(t *testing.T) {
	var rt *ResolvedType
	if rt.Role() != RoleNone {
		t.Error("nil ResolvedType.Role() should be RoleNone")
	}
	empty := &ResolvedType{}
	if empty.Role() != RoleNone {
		t.Error("schemaless ResolvedType.Role() should be RoleNone")
	}
}

// TestBehaviorPositionalFoldIn verifies the positional fold-in: IsPositional()
// returns true for BOTH the legacy `positional: true` keyword AND a new
// `role: region` behavior, so nothing downstream this story breaks.
func TestBehaviorPositionalFoldIn(t *testing.T) {
	legacy := resolvedFromJSON(t, `{
		"$id": "https://lattice.dev/schemas/items/legacy-region/1.0.0",
		"type": "object",
		"positional": true
	}`)
	if !legacy.IsPositional() {
		t.Error("legacy positional:true IsPositional() = false, want true")
	}
	if legacy.Role() != RoleNone {
		t.Errorf("legacy positional Role() = %q, want RoleNone (no behavior keyword)", legacy.Role())
	}

	roleRegion := resolvedFromJSON(t, `{
		"$id": "https://lattice.dev/schemas/items/new-region/1.0.0",
		"type": "object",
		"latticeBehavior": { "role": "region" }
	}`)
	if !roleRegion.IsPositional() {
		t.Error("role:region IsPositional() = false, want true (folded in)")
	}

	wrapper := resolvedFromJSON(t, `{
		"$id": "https://lattice.dev/schemas/items/new-wrapper/1.0.0",
		"type": "object",
		"latticeBehavior": { "role": "wrapper", "contentField": "content" }
	}`)
	if wrapper.IsPositional() {
		t.Error("role:wrapper IsPositional() = true, want false")
	}

	plain := resolvedFromJSON(t, `{
		"$id": "https://lattice.dev/schemas/items/plain/1.0.0",
		"type": "object"
	}`)
	if plain.IsPositional() {
		t.Error("plain leaf IsPositional() = true, want false")
	}
}
