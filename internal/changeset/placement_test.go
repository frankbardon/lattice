package changeset

import (
	"encoding/json"
	"testing"

	"github.com/frankbardon/lattice/errors"
)

// placementOf navigates the canonical output to the `placement` map of the instance
// declaring id, searching the same physical instance-bearing locations the
// translator indexes (root, child slots, and a block wrapper's config/content
// leaf). It fails the test if the node or its placement is absent.
func placementOf(t *testing.T, doc []byte, id string) map[string]any {
	t.Helper()
	var root map[string]any
	if err := json.Unmarshal(doc, &root); err != nil {
		t.Fatalf("decode result doc: %v", err)
	}
	inst := findInstance(root["root"].(map[string]any), id)
	if inst == nil {
		t.Fatalf("instance %q not found in result doc", id)
	}
	p, ok := inst[placementKey].(map[string]any)
	if !ok {
		t.Fatalf("instance %q has no placement object: %v", id, inst[placementKey])
	}
	return p
}

// TestApplyToBytes_PlacementCoordinateApplies proves an id-rooted placement
// COORDINATE edit (`/<id>/placement/colStart`) — documented as valid in
// placement-grid.md — is accepted past the configurable-surface guardrail and
// PERSISTS the new value. `fruits-block` starts at colStart=1 in `body`'s
// two-column grid; replacing it with colStart=2 stays in bounds.
func TestApplyToBytes_PlacementCoordinateApplies(t *testing.T) {
	docBytes, tree := resolveFixture(t)

	cs := parse(t, `[{"op":"replace","path":"/fruits-block/placement/colStart","value":2}]`)
	out, err := applyToBytes(docBytes, cs, tree)
	if err != nil {
		t.Fatalf("replace placement coordinate should apply: %v", err)
	}

	p := placementOf(t, out, "fruits-block")
	if p["colStart"] != float64(2) {
		t.Fatalf("placement.colStart after replace = %v, want 2", p["colStart"])
	}
	// Sibling coordinates are untouched by the single-coordinate edit.
	if p["colSpan"] != float64(1) || p["rowStart"] != float64(1) {
		t.Fatalf("single-coordinate edit disturbed siblings: %v", p)
	}
}

// TestApplyToBytes_PlacementWholeObjectApplies proves a WHOLE-OBJECT placement
// edit (`/<id>/placement`) is likewise routed past the surface guardrail and
// persists. The replacement coordinates stay within `body`'s two-column grid.
func TestApplyToBytes_PlacementWholeObjectApplies(t *testing.T) {
	docBytes, tree := resolveFixture(t)

	cs := parse(t, `[{"op":"replace","path":"/fruits-block/placement","value":{"colStart":2,"colSpan":1,"rowStart":1,"rowSpan":1}}]`)
	out, err := applyToBytes(docBytes, cs, tree)
	if err != nil {
		t.Fatalf("replace whole placement should apply: %v", err)
	}

	p := placementOf(t, out, "fruits-block")
	if p["colStart"] != float64(2) {
		t.Fatalf("placement.colStart after whole-object replace = %v, want 2", p["colStart"])
	}
}

// TestApplyToBytes_OutOfBoundsPlacementRejectedByReResolve proves the apply stage
// does NOT enforce grid bounds — it lets the out-of-bounds coordinate through
// (ungated, exactly like metadata) — but re-resolving the MUTATED document rejects
// it with LAYOUT_PLACEMENT_OUT_OF_BOUNDS. `body` has only two columns, so
// colStart=3 is off the declared tracks; the placement is therefore NOT silently
// accepted by the pipeline.
func TestApplyToBytes_OutOfBoundsPlacementRejectedByReResolve(t *testing.T) {
	docBytes, tree := resolveFixture(t)

	cs := parse(t, `[{"op":"replace","path":"/fruits-block/placement/colStart","value":3}]`)
	out, err := applyToBytes(docBytes, cs, tree)
	if err != nil {
		t.Fatalf("applyToBytes should not reject an out-of-bounds placement at the apply stage: %v", err)
	}
	// The mutated bytes carry the (out-of-bounds) coordinate; re-resolve is the gate.
	if p := placementOf(t, out, "fruits-block"); p["colStart"] != float64(3) {
		t.Fatalf("placement should have been applied (ungated) before re-resolve: %v", p)
	}
	reResolveRejects(t, out, errors.LAYOUT_PLACEMENT_OUT_OF_BOUNDS)
}

// TestApplyToBytes_UnknownFieldStillRejected guards against the placement bypass
// loosening anything else: a genuinely unknown config field — neither on the
// configurable surface nor one of the envelope siblings (config/metadata/placement/
// children) — is STILL rejected at the apply stage with
// CONFIG_OVERRIDE_FIELD_UNKNOWN. `fruits`'s `bogus` is not a surfaced field.
func TestApplyToBytes_UnknownFieldStillRejected(t *testing.T) {
	docBytes, tree := resolveFixture(t)

	cs := parse(t, `[{"op":"replace","path":"/fruits/config/bogus","value":1}]`)
	_, err := applyToBytes(docBytes, cs, tree)
	hasCode(t, err, errors.CONFIG_OVERRIDE_FIELD_UNKNOWN)
}
