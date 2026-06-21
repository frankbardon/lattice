package service_test

// This file is the COMPILE-CHECKED walkthrough for MCP mode (docs/src/reference/mcp.md).
// The lattice MCP tools (list_dashboards, get_outline, get_node, get_document,
// list_schemas, get_schema, validate_patch) are thin wrappers over the public
// ./service facade — the SAME boundary an external Go module sees. This example
// drives exactly the facade calls those tools make, in propose-then-commit order,
// so the documented tool contract cannot drift from the real facade signatures:
// if a signature changes, this example stops compiling.
//
// Like the other facade examples it imports ONLY the public surface (service +
// stdlib) — no internal/... path — and it NEVER persists, mirroring the MCP
// server's read + dry-run-only contract (Save/Patch/Delete are deliberately not
// called here).

import (
	"fmt"
	"os"

	"github.com/frankbardon/lattice/service"
)

// Example_mcpProposeThenCommit walks the read -> drill/discover -> validate loop
// the MCP tools expose, ending at the dry-run (validate_patch) that the model
// stops at — the human commits the validated patch out of band via POST /api/patch.
func Example_mcpProposeThenCommit() {
	// Seed a fresh store with one example document (a real deployment points
	// --root at its own directory of <id>.json files; here we stage one).
	root, _ := os.MkdirTemp("", "lattice-mcp")
	defer os.RemoveAll(root)
	doc, _ := os.ReadFile("../examples/minimal-dashboard.json")
	_ = os.WriteFile(root+"/example-minimal.json", doc, 0o644)

	// The same Open seam `lattice mcp` uses: a store rooted at --root plus a
	// resolver over the --schemas catalog. The MCP tools call only through this.
	svc, err := service.Open(service.Options{
		Backend: service.BackendFS, // BackendGit adds a per-write commit + history
		Root:    root,
		Schemas: os.DirFS("../schemas"),
	})
	if err != nil {
		fmt.Println("open:", err)
		return
	}

	// 1) list_dashboards -> svc.List(): discover the stored document ids.
	ids, err := svc.List()
	if err != nil {
		fmt.Println("list:", err)
		return
	}
	id := ids[0]

	// 2) get_outline -> svc.Resolve(id, nil) + svc.Revision(id): the config-free
	//    skeleton is projected from the resolved tree; the revision is best-effort
	//    (a store without the capability simply omits it). Here we use it to locate
	//    the root node id to drill into.
	tree, err := svc.Resolve(id, nil)
	if err != nil {
		fmt.Println("resolve:", err)
		return
	}
	nodeID := tree.Root.ID
	if rev, rerr := svc.Revision(id); rerr == nil {
		_ = rev // outline carries this so a later write can pass expectedRevision
	}

	// 3a) get_node -> svc.NodeView(id, nodeID): the stored subtree (the shape a
	//     patch edits) plus the editable field surface (which field paths are
	//     valid to patch) for editing an EXISTING node.
	view, err := svc.NodeView(id, nodeID)
	if err != nil {
		fmt.Println("node:", err)
		return
	}
	_ = view.Subtree
	_ = view.Surface

	// 3b) list_schemas -> svc.ListSchemas() and get_schema -> svc.Schema(type):
	//     the grammar catalog + one type's JSON Schema, for BUILDING a new node.
	types, err := svc.ListSchemas()
	if err != nil {
		fmt.Println("schemas:", err)
		return
	}
	if _, err := svc.Schema(types[0]); err != nil {
		fmt.Println("schema:", err)
		return
	}

	// 4) validate_patch -> svc.ParseChangeset(ops) + svc.DryRunPatch(id, cs):
	//    simulate an id-rooted RFC 6902 patch under every guardrail WITHOUT
	//    persisting. The model iterates this until it succeeds. Pointers are
	//    id-rooted; document scopes use a "$"-prefixed token ("/$manifest/title").
	cs, err := svc.ParseChangeset([]byte(
		`[{"op":"replace","path":"/$manifest/title","value":"Proposed"}]`))
	if err != nil {
		fmt.Println("parse:", err)
		return
	}
	preview, err := svc.DryRunPatch(id, cs)
	if err != nil {
		fmt.Println("dry-run:", err)
		return
	}

	// The dry-run returns the resolved tree the patch WOULD produce — nothing was
	// written. The store is untouched: the model proposes; a human commits via
	// POST /api/patch (passing the document's current revision as expectedRevision).
	fmt.Printf("preview title=%v persisted=%v\n",
		preview.Resolved.Manifest["title"],
		mcpStoredTitle(svc, id))
	// Output: preview title=Proposed persisted=Minimal Example Dashboard
}

// mcpStoredTitle re-reads the STORED manifest title to prove the dry-run persisted
// nothing — the stored title is still the original, not the proposed one.
func mcpStoredTitle(svc *service.Service, id string) any {
	tree, err := svc.Resolve(id, nil)
	if err != nil {
		return err
	}
	return tree.Manifest["title"]
}
