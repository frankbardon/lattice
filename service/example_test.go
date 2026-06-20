package service_test

// This file is the COMPILE-CHECKED import example for the public facade. It lives
// in the black-box `service_test` package and imports ONLY what an external Go
// module has available: the public service facade, stdlib, and afero. No
// internal/... path appears. Because these are real Go example functions, the
// compiler keeps them honest — if a signature on the facade changes, these stop
// compiling, so the documented usage cannot rot silently.

import (
	"fmt"
	"os"

	"github.com/spf13/afero"

	"github.com/frankbardon/lattice/service"
)

// Example_open shows the batteries-included path an external program uses against
// a real filesystem: Open wires a store rooted at a directory plus a resolver
// over a schema-catalog fs.FS (here os.DirFS), then Resolve loads a document by
// its manifest.id and runs the two-pass resolver. Patch (omitted here, see
// Example_inject) follows the same shape: ParseChangeset then svc.Patch(id, cs).
func Example_open() {
	// The FS backend addresses documents by manifest.id, so each lives at
	// <root>/<id>.json. Seed one example document into a fresh root. (A real
	// program points Root at its own document directory; documents arrive there
	// via svc.Save or out of band.)
	root, _ := os.MkdirTemp("", "lattice-docs")
	defer os.RemoveAll(root)
	doc, _ := os.ReadFile("../examples/minimal-dashboard.json")
	_ = os.WriteFile(root+"/example-minimal.json", doc, 0o644)

	svc, err := service.Open(service.Options{
		Backend: service.BackendFS,      // BackendGit adds commit-per-write history
		Root:    root,                   // directory of <id>.json documents
		Schemas: os.DirFS("../schemas"), // holds dashboard.schema.json + the catalog
	})
	if err != nil {
		fmt.Println("open:", err)
		return
	}

	// Resolve loads <root>/example-minimal.json by manifest.id and validates it.
	// The second argument is the runtime override map (nil applies none).
	tree, err := svc.Resolve("example-minimal", nil)
	if err != nil {
		fmt.Println("resolve:", err)
		return
	}

	fmt.Printf("id=%v root=%s\n", tree.Manifest["id"], tree.Root.ID)
	// Output: id=example-minimal root=root
}

// Example_inject shows the injection path future WASM/MCP frontends use: build a
// Store and a Resolver yourself (here an in-memory afero store and a resolver
// over the on-disk schema catalog — a host could pass an embed.FS instead) and
// wire them with New, never touching Open. It then walks the full write verb set:
// Save raw bytes, ParseChangeset + Patch for a validated RFC 6902 edit.
func Example_inject() {
	// A caller-supplied Store. Any backend works; an in-memory afero.Fs keeps the
	// example hermetic. A custom Store implementation can be injected just as well.
	store, err := service.NewStore(service.BackendFS, afero.NewMemMapFs(), "docs")
	if err != nil {
		fmt.Println("store:", err)
		return
	}

	// A resolver over the schema catalog. os.DirFS here; a WASM host substitutes
	// an embed.FS sub-tree whose top level holds dashboard.schema.json.
	res, err := service.NewResolver(os.DirFS("../schemas"))
	if err != nil {
		fmt.Println("resolver:", err)
		return
	}

	svc := service.New(store, res)

	// Seed a document. Save writes UNVALIDATED bytes; the store addresses by the
	// document's own manifest.id, so the bytes alone determine where they land.
	doc, err := os.ReadFile("../examples/minimal-dashboard.json")
	if err != nil {
		fmt.Println("read:", err)
		return
	}
	if err := svc.Save(doc); err != nil {
		fmt.Println("save:", err)
		return
	}

	// Apply a validated RFC 6902 edit: parse to an opaque *Changeset, then Patch.
	// Patch runs the atomic load -> resolve -> apply -> re-resolve -> save pipeline
	// and persists only if every guardrail passes. Pointers are id-rooted: a node
	// is named by its id ("/body/config/grid/gap"), and document scopes by a
	// "$"-prefixed token ("/$manifest/title" here, "/$theme/..." for the theme).
	cs, err := svc.ParseChangeset([]byte(`[{"op":"replace","path":"/$manifest/title","value":"Renamed"}]`))
	if err != nil {
		fmt.Println("parse:", err)
		return
	}
	res2, err := svc.Patch("example-minimal", cs)
	if err != nil {
		fmt.Println("patch:", err)
		return
	}

	fmt.Printf("title=%v\n", res2.Resolved.Manifest["title"])
	// Output: title=Renamed
}
