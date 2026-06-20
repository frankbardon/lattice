package service_test

// This file is the EXTERNAL-CALLER proof for the read path (E1-S4). It lives in
// the black-box `service_test` package and imports ONLY the public facade
// (github.com/frankbardon/lattice/service), the public errors package, stdlib,
// the test framework, and afero — exactly the dependency set a future WASM/MCP
// frontend has available. NO internal/... import appears here; if one were
// needed to drive a read, the facade would have a gap.

import (
	"os"
	"testing"

	"github.com/spf13/afero"

	"github.com/frankbardon/lattice/service"
)

// fixtureID is the manifest.id of the seeded fixture document; the store
// addresses documents by manifest.id, so this is the key Load/Resolve use.
const fixtureID = "example-minimal"

// loadFixtureDoc returns the bytes of the known-good example document. The same
// schema-valid document drives both the in-memory injection path and the Open
// path, so the two tests assert against identical resolved output. It is read
// from the repo's examples/ rather than embedded inline so the fixture stays in
// lockstep with the dashboard grammar.
func loadFixtureDoc(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile("../examples/minimal-dashboard.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return b
}

// TestExternalInjectionReadPath constructs a Service the way an external module
// would: a caller-supplied in-memory Store (afero MemMapFs via NewStore) seeded
// through the Store's own Save, plus a resolver built over the repo's real
// schema catalog via NewResolver. It then asserts the full read surface —
// Resolve, ResolveBytes, Load, List, Exists — behaves against that wiring.
func TestExternalInjectionReadPath(t *testing.T) {
	// In-memory store, constructed only via the facade (no internal import).
	store, err := service.NewStore(service.BackendFS, afero.NewMemMapFs(), "docs")
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	// Seed the fixture via the Store's Save (E1-S1's aliased interface). The
	// store extracts manifest.id and writes <id>.json under its root.
	fixtureDoc := loadFixtureDoc(t)
	if err := store.Save(fixtureDoc); err != nil {
		t.Fatalf("store.Save: %v", err)
	}

	// Resolver over the real schema catalog, rooted where dashboard.schema.json
	// sits (see construct.go's schema-rooting assumption).
	res, err := service.NewResolver(os.DirFS("../schemas"))
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}

	svc := service.New(store, res)

	// --- Exists ---
	ok, err := svc.Exists(fixtureID)
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if !ok {
		t.Fatalf("Exists(%q) = false, want true after Save", fixtureID)
	}
	if ok, err := svc.Exists("does-not-exist"); err != nil {
		t.Fatalf("Exists(missing): %v", err)
	} else if ok {
		t.Fatal("Exists(\"does-not-exist\") = true, want false")
	}

	// --- List ---
	ids, err := svc.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(ids) != 1 || ids[0] != fixtureID {
		t.Fatalf("List() = %v, want [%q]", ids, fixtureID)
	}

	// --- Load (byte-faithful passthrough) ---
	raw, err := svc.Load(fixtureID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if string(raw) != string(fixtureDoc) {
		t.Fatalf("Load returned non-byte-faithful document:\n got %q\nwant %q", raw, fixtureDoc)
	}

	// --- Resolve (store-addressed load + two-pass resolve) ---
	tree, err := svc.Resolve(fixtureID, nil)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	assertFixtureTree(t, tree)

	// --- ResolveBytes (in-memory, no store touch) ---
	btree, err := svc.ResolveBytes(raw, fixtureID, nil)
	if err != nil {
		t.Fatalf("ResolveBytes: %v", err)
	}
	assertFixtureTree(t, btree)
}

// TestExternalOpenReadPath exercises the batteries-included Open constructor
// against the repo's real schemas/ and a real example document, proving parity
// with the direct injection path: a successful resolve of a known-good example.
func TestExternalOpenReadPath(t *testing.T) {
	// Seed the real example doc into a temp store root so Open's FS backend can
	// load it by manifest.id. Open wires an OsFs-rooted store + the schema
	// resolver; we only supply the document and the schema catalog path.
	root := t.TempDir()
	exampleBytes := loadFixtureDoc(t)
	if err := os.WriteFile(root+"/"+fixtureID+".json", exampleBytes, 0o644); err != nil {
		t.Fatalf("seed example: %v", err)
	}

	svc, err := service.Open(service.Options{
		Backend: service.BackendFS,
		Root:    root,
		Schemas: os.DirFS("../schemas"),
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	tree, err := svc.Resolve(fixtureID, nil)
	if err != nil {
		t.Fatalf("Resolve via Open: %v", err)
	}
	assertFixtureTree(t, tree)

	// Parity: ResolveBytes over the same example bytes yields an equivalent tree.
	btree, err := svc.ResolveBytes(exampleBytes, fixtureID, nil)
	if err != nil {
		t.Fatalf("ResolveBytes via Open: %v", err)
	}
	assertFixtureTree(t, btree)
}

// assertFixtureTree checks the resolved tree carries the fixture's manifest id
// and a resolved container root — the stable, downstream-facing shape of the
// resolved output. It does not over-assert internal node structure.
func assertFixtureTree(t *testing.T, tree *service.ResolvedTree) {
	t.Helper()
	if tree == nil {
		t.Fatal("resolved tree is nil")
	}
	if got, _ := tree.Manifest["id"].(string); got != fixtureID {
		t.Fatalf("Manifest[\"id\"] = %v, want %q", tree.Manifest["id"], fixtureID)
	}
	if tree.Root == nil {
		t.Fatal("resolved tree Root is nil")
	}
	if tree.Root.ID != "root" {
		t.Fatalf("Root.ID = %q, want \"root\"", tree.Root.ID)
	}
	if !tree.Root.Container {
		t.Fatal("Root.Container = false, want true (root is a container)")
	}
}
