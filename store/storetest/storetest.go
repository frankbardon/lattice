// Package storetest provides a reusable conformance suite for store.Store
// implementations. A future Postgres impl can run the same suite to prove it
// behaves identically to the SQLite one.
package storetest

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/frankbardon/lattice/dashboard"
	"github.com/frankbardon/lattice/store"
)

// Factory builds a fresh, empty Store for a single test. The returned cleanup
// is called via t.Cleanup by the suite.
type Factory func(t *testing.T) (store.Store, func())

// Run executes the full conformance suite against stores produced by newStore.
func Run(t *testing.T, newStore Factory) {
	t.Helper()
	t.Run("RoundTrip", func(t *testing.T) { testRoundTrip(t, newStore) })
	t.Run("CreateDuplicate", func(t *testing.T) { testCreateDuplicate(t, newStore) })
	t.Run("LoadMissing", func(t *testing.T) { testLoadMissing(t, newStore) })
	t.Run("Save", func(t *testing.T) { testSave(t, newStore) })
	t.Run("List", func(t *testing.T) { testList(t, newStore) })
	t.Run("Delete", func(t *testing.T) { testDelete(t, newStore) })
}

func sample(id string) *dashboard.Dashboard {
	return &dashboard.Dashboard{
		ID:   id,
		Name: "Board " + id,
		Variables: []dashboard.Variable{
			{Name: "region", Value: "us-east"},
		},
		Bricks: []dashboard.Brick{
			{
				ID:       "brick-1",
				Kind:     "markdown",
				Layout:   dashboard.Layout{Pos: dashboard.Position{X: 1, Y: 2}, Size: dashboard.Size{Width: 3, Height: 4}},
				Template: "# ${region}",
				AgentID:  "agent-1",
			},
		},
	}
}

func newSuiteStore(t *testing.T, newStore Factory) store.Store {
	t.Helper()
	s, cleanup := newStore(t)
	t.Cleanup(cleanup)
	return s
}

func testRoundTrip(t *testing.T, newStore Factory) {
	t.Helper()
	ctx := context.Background()
	s := newSuiteStore(t, newStore)

	in := sample("d1")
	if err := s.Create(ctx, in); err != nil {
		t.Fatalf("Create: %v", err)
	}
	out, err := s.Load(ctx, "d1")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	assertEqual(t, in, out)
}

func testCreateDuplicate(t *testing.T, newStore Factory) {
	t.Helper()
	ctx := context.Background()
	s := newSuiteStore(t, newStore)

	if err := s.Create(ctx, sample("dup")); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	err := s.Create(ctx, sample("dup"))
	if got := store.CodeOf(err); got != store.Exists {
		t.Fatalf("duplicate Create code = %q, want %q (err=%v)", got, store.Exists, err)
	}
}

func testLoadMissing(t *testing.T, newStore Factory) {
	t.Helper()
	ctx := context.Background()
	s := newSuiteStore(t, newStore)

	_, err := s.Load(ctx, "nope")
	if got := store.CodeOf(err); got != store.NotFound {
		t.Fatalf("Load missing code = %q, want %q (err=%v)", got, store.NotFound, err)
	}
}

func testSave(t *testing.T, newStore Factory) {
	t.Helper()
	ctx := context.Background()
	s := newSuiteStore(t, newStore)

	// Save acts as create when absent.
	doc := sample("s1")
	if err := s.Save(ctx, doc); err != nil {
		t.Fatalf("Save (create): %v", err)
	}
	// Save updates when present.
	doc.Name = "Renamed"
	if err := s.Save(ctx, doc); err != nil {
		t.Fatalf("Save (update): %v", err)
	}
	out, err := s.Load(ctx, "s1")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if out.Name != "Renamed" {
		t.Fatalf("Name = %q, want %q", out.Name, "Renamed")
	}
}

func testList(t *testing.T, newStore Factory) {
	t.Helper()
	ctx := context.Background()
	s := newSuiteStore(t, newStore)

	if got, err := s.List(ctx); err != nil {
		t.Fatalf("List empty: %v", err)
	} else if len(got) != 0 {
		t.Fatalf("List empty len = %d, want 0", len(got))
	}

	for _, id := range []string{"b", "a", "c"} {
		if err := s.Create(ctx, sample(id)); err != nil {
			t.Fatalf("Create %q: %v", id, err)
		}
	}
	got, err := s.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("List len = %d, want 3", len(got))
	}
	want := []string{"a", "b", "c"}
	for i, d := range got {
		if d.ID != want[i] {
			t.Fatalf("List[%d].ID = %q, want %q (not ordered)", i, d.ID, want[i])
		}
	}
}

func testDelete(t *testing.T, newStore Factory) {
	t.Helper()
	ctx := context.Background()
	s := newSuiteStore(t, newStore)

	if err := s.Create(ctx, sample("del")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := s.Delete(ctx, "del"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Load(ctx, "del"); store.CodeOf(err) != store.NotFound {
		t.Fatalf("Load after Delete code = %q, want %q", store.CodeOf(err), store.NotFound)
	}
	// Deleting again is NotFound.
	if err := s.Delete(ctx, "del"); store.CodeOf(err) != store.NotFound {
		t.Fatalf("Delete missing code = %q, want %q", store.CodeOf(err), store.NotFound)
	}
}

// assertEqual compares two dashboards field-by-field with helpful messages.
func assertEqual(t *testing.T, want, got *dashboard.Dashboard) {
	t.Helper()
	if got.ID != want.ID || got.Name != want.Name {
		t.Fatalf("dashboard header = {%q,%q}, want {%q,%q}", got.ID, got.Name, want.ID, want.Name)
	}
	if len(got.Variables) != len(want.Variables) {
		t.Fatalf("variables len = %d, want %d", len(got.Variables), len(want.Variables))
	}
	for i := range want.Variables {
		// Variable carries an Options slice, so it is no longer comparable with
		// ==; compare its JSON form instead (the stored shape is what matters).
		if !jsonEqual(got.Variables[i], want.Variables[i]) {
			t.Fatalf("variables[%d] = %+v, want %+v", i, got.Variables[i], want.Variables[i])
		}
	}
	if len(got.Bricks) != len(want.Bricks) {
		t.Fatalf("bricks len = %d, want %d", len(got.Bricks), len(want.Bricks))
	}
	for i := range want.Bricks {
		if got.Bricks[i] != want.Bricks[i] {
			t.Fatalf("bricks[%d] = %+v, want %+v", i, got.Bricks[i], want.Bricks[i])
		}
	}
}

// jsonEqual reports whether two values have identical JSON encodings. Used to
// compare values that are no longer == comparable (e.g. they hold a slice).
func jsonEqual(a, b any) bool {
	ra, _ := json.Marshal(a)
	rb, _ := json.Marshal(b)
	return string(ra) == string(rb)
}
