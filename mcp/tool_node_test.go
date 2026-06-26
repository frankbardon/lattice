package mcp

// Typed-handler + descriptor tests for get_node (E2-S1), ported from the legacy
// internal/mcp/tools_node_test.go but driven WITHOUT a server. They confirm:
// get_node on a BLOCK id returns the whole block subtree (wrapper plus
// config/content, including the wrapped table's config), the surface lists the
// CONTENT item's editable fields, a revision is present (the fs backend is a
// RevisionedStore), and an unknown document id vs an unknown node id give DISTINCT
// coded errors.

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/frankbardon/lattice/errors"
)

// TestGetNodeRegistered asserts get_node is present in the Tools() catalog with a
// reflection-generated input+output schema — i.e. NewTool reflected the `any`
// subtree + flat surface without panicking.
func TestGetNodeRegistered(t *testing.T) {
	d := findDescriptor(t, "get_node")
	if d.Description != getNodeDescription {
		t.Errorf("description mismatch:\n got %q\nwant %q", d.Description, getNodeDescription)
	}
	if len(d.InputSchema) == 0 {
		t.Errorf("get_node has empty InputSchema (expected reflection-generated)")
	}
	if len(d.OutputSchema) == 0 {
		t.Errorf("get_node has empty OutputSchema (expected reflection-generated)")
	}
}

// TestGetNodeBlock asserts get_node on a block id returns the whole block subtree
// (wrapper + config/content with the wrapped table's config), surfaces the CONTENT
// item's editable fields, and includes a revision. It drives the descriptor's
// erased Invoke so the wire shape is exercised end to end.
func TestGetNodeBlock(t *testing.T) {
	svc := newTestService(t)
	d := findDescriptor(t, "get_node")

	raw, err := d.Invoke(context.Background(), svc, json.RawMessage(`{"id":"`+fixtureID+`","nodeId":"fruits-block"}`))
	if err != nil {
		t.Fatalf("Invoke get_node: %v", err)
	}

	var out struct {
		ID       string          `json:"id"`
		NodeID   string          `json:"nodeId"`
		Revision string          `json:"revision"`
		Subtree  json.RawMessage `json:"subtree"`
		Surface  []struct {
			Key  string `json:"key"`
			Type string `json:"type"`
		} `json:"surface"`
	}
	remarshal(t, raw, &out)

	if out.ID != fixtureID {
		t.Errorf("id = %q, want %q", out.ID, fixtureID)
	}
	if out.NodeID != "fruits-block" {
		t.Errorf("nodeId = %q, want %q", out.NodeID, "fruits-block")
	}
	// The fs backend is a RevisionedStore, so a revision must be present.
	if out.Revision == "" {
		t.Errorf("revision is empty, want the store's current revision token")
	}

	// The subtree is the WHOLE block: wrapper id + content table config.
	var subtree struct {
		ID     string `json:"id"`
		Config struct {
			ID      string `json:"id"`
			Content struct {
				ID     string `json:"id"`
				Config struct {
					Title   string `json:"title"`
					Columns []any  `json:"columns"`
				} `json:"config"`
			} `json:"content"`
		} `json:"config"`
	}
	if err := json.Unmarshal(out.Subtree, &subtree); err != nil {
		t.Fatalf("unmarshal subtree: %v\n%s", err, out.Subtree)
	}
	if subtree.ID != "fruits-block" {
		t.Errorf("subtree wrapper id = %q, want %q", subtree.ID, "fruits-block")
	}
	if subtree.Config.Content.ID != "fruits" {
		t.Errorf("subtree content id = %q, want %q (block subtree must include config/content)", subtree.Config.Content.ID, "fruits")
	}
	if subtree.Config.Content.Config.Title != "Fruits" {
		t.Errorf("subtree content title = %q, want %q", subtree.Config.Content.Config.Title, "Fruits")
	}

	// The surface is the CONTENT (table) item's editable fields, not the block's.
	keys := map[string]string{}
	for _, f := range out.Surface {
		keys[f.Key] = f.Type
	}
	for _, want := range []string{"title", "columns"} {
		if _, ok := keys[want]; !ok {
			t.Errorf("surface missing content field %q; got %v", want, keys)
		}
	}
	// The block's own surface fields (title/visibility live on the wrapper) must NOT
	// stand in for the content's surface: the content table declares no "visibility"
	// field, so its presence would mean we surfaced the wrapper by mistake.
	if _, ok := keys["visibility"]; ok {
		t.Errorf("surface carries the block wrapper's 'visibility' field; want the content item's surface only: %v", keys)
	}
}

// TestGetNodeUnknownIDDistinctError asserts an unknown document id surfaces
// STORAGE_NOT_FOUND verbatim.
func TestGetNodeUnknownIDDistinctError(t *testing.T) {
	svc := newTestService(t)

	_, err := getNode(context.Background(), svc, getNodeInput{ID: "does-not-exist", NodeID: "fruits-block"})
	if err == nil {
		t.Fatalf("expected an error for unknown document id, got success")
	}
	if !errors.HasCode(err, errors.STORAGE_NOT_FOUND) {
		t.Errorf("error = %v, want it to carry STORAGE_NOT_FOUND", err)
	}
}

// TestGetNodeUnknownNodeDistinctError asserts an unknown node id surfaces
// CHANGESET_TARGET_NOT_FOUND — DISTINCT from the unknown-document-id error.
func TestGetNodeUnknownNodeDistinctError(t *testing.T) {
	svc := newTestService(t)

	_, err := getNode(context.Background(), svc, getNodeInput{ID: fixtureID, NodeID: "no-such-node"})
	if err == nil {
		t.Fatalf("expected an error for unknown node id, got success")
	}
	if !errors.HasCode(err, errors.CHANGESET_TARGET_NOT_FOUND) {
		t.Errorf("error = %v, want it to carry CHANGESET_TARGET_NOT_FOUND", err)
	}
	// Distinct from the unknown-document-id code.
	if errors.HasCode(err, errors.STORAGE_NOT_FOUND) {
		t.Errorf("unknown-node error also carried STORAGE_NOT_FOUND; the two cases must be distinct: %v", err)
	}
}
