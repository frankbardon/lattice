package mcp

// Typed-handler + descriptor tests for get_outline (E2-S1), ported from the legacy
// internal/mcp/tools_outline_test.go but driven WITHOUT a server: the typed handler
// getOutline is invoked directly, and the descriptor's erased Invoke is exercised
// off the Tools() catalog. The assertions preserve the legacy behavior: the outline
// carries the expected node ids in the expected tree shape, includes a revision
// (the fs backend is a RevisionedStore), and — crucially — carries NO config bodies.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/frankbardon/lattice/errors"
)

// decodedOutlineNode mirrors the tool's config-free skeleton for decoding. Children
// is decoded recursively; the handler emits children as a nested array.
type decodedOutlineNode struct {
	ID        string               `json:"id"`
	Type      string               `json:"type"`
	Title     string               `json:"title"`
	Container bool                 `json:"container"`
	Placement string               `json:"placement"`
	Metadata  map[string]any       `json:"metadata"`
	Children  []decodedOutlineNode `json:"children"`
}

// decodedOutline mirrors getOutlineOutput on the wire.
type decodedOutline struct {
	ID       string `json:"id"`
	Revision string `json:"revision"`
	Document struct {
		Variables   []string `json:"variables"`
		Connections []string `json:"connections"`
		Theme       bool     `json:"theme"`
	} `json:"document"`
	Root *decodedOutlineNode `json:"root"`
}

// TestGetOutlineRegistered asserts get_outline is present in the Tools() catalog
// with a reflection-generated input+output schema — i.e. NewTool reflected the
// recursive skeleton without panicking.
func TestGetOutlineRegistered(t *testing.T) {
	d := findDescriptor(t, "lattice_get_outline")
	if d.Description != getOutlineDescription {
		t.Errorf("description mismatch:\n got %q\nwant %q", d.Description, getOutlineDescription)
	}
	if len(d.InputSchema) == 0 {
		t.Errorf("get_outline has empty InputSchema (expected reflection-generated)")
	}
	if len(d.OutputSchema) == 0 {
		t.Errorf("get_outline has empty OutputSchema (expected reflection-generated)")
	}
}

// TestGetOutline asserts the outline mirrors the fixture tree (ids + shape),
// carries a title only where the config declares one, includes a revision, and
// carries NO config bodies. It drives the descriptor's erased Invoke so the wire
// shape (the same a transport adapter dispatches) is exercised end to end.
func TestGetOutline(t *testing.T) {
	svc := newTestService(t)
	d := findDescriptor(t, "lattice_get_outline")

	raw, err := d.Invoke(context.Background(), svc, json.RawMessage(`{"id":"`+fixtureID+`"}`))
	if err != nil {
		t.Fatalf("Invoke get_outline: %v", err)
	}

	var out decodedOutline
	remarshal(t, raw, &out)

	if out.ID != fixtureID {
		t.Errorf("id = %q, want %q", out.ID, fixtureID)
	}
	// The fs backend is a RevisionedStore, so a revision must be present.
	if out.Revision == "" {
		t.Errorf("revision is empty, want the store's current revision token")
	}
	if out.Root == nil {
		t.Fatalf("root is nil")
	}

	// Collect every node id and title in the skeleton.
	ids := map[string]bool{}
	titles := map[string]string{}
	var walk func(n *decodedOutlineNode)
	walk = func(n *decodedOutlineNode) {
		if n.ID != "" {
			ids[n.ID] = true
			titles[n.ID] = n.Title
		}
		for i := range n.Children {
			walk(&n.Children[i])
		}
	}
	walk(out.Root)

	// The fixture's full id set across the nested tree.
	for _, want := range []string{"root", "body", "fruits-block", "fruits", "metrics-block", "metrics"} {
		if !ids[want] {
			t.Errorf("outline missing expected node id %q", want)
		}
	}

	// Tree shape: root -> body -> two blocks, each block -> one table.
	if got := out.Root.ID; got != "root" {
		t.Errorf("root id = %q, want %q", got, "root")
	}
	if len(out.Root.Children) != 1 || out.Root.Children[0].ID != "body" {
		t.Fatalf("root children = %+v, want single 'body'", out.Root.Children)
	}
	body := out.Root.Children[0]
	if len(body.Children) != 2 {
		t.Fatalf("body children = %d, want 2", len(body.Children))
	}

	// Title present only where the config declares one: the tables carry titles, the
	// structural containers/blocks do not.
	if titles["fruits"] != "Fruits" {
		t.Errorf("fruits title = %q, want %q", titles["fruits"], "Fruits")
	}
	if titles["metrics"] != "Metrics" {
		t.Errorf("metrics title = %q, want %q", titles["metrics"], "Metrics")
	}
	if titles["root"] != "" {
		t.Errorf("root title = %q, want empty (container declares no title)", titles["root"])
	}

	// Container flag carried through.
	if !out.Root.Container {
		t.Errorf("root.container = false, want true")
	}

	// A placed block carries a compact placement summary, not the raw object.
	var firstBlock *decodedOutlineNode
	for i := range body.Children {
		if body.Children[i].ID == "fruits-block" {
			firstBlock = &body.Children[i]
		}
	}
	if firstBlock == nil {
		t.Fatalf("fruits-block not found under body")
	}
	if firstBlock.Placement == "" {
		t.Errorf("fruits-block placement summary is empty, want a compact summary")
	}

	// CONFIG-FREE assertion: re-marshal the whole result and confirm no config field
	// values from the fixture leak through. The table config carries distinctive
	// values ("Apple", "Banana", column header "Color", "rows") that must NOT appear
	// anywhere in the outline.
	b, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	wire := string(b)
	for _, leak := range []string{"Apple", "Banana", "Color", "\"rows\"", "\"columns\"", "\"grid\"", "\"config\""} {
		if strings.Contains(wire, leak) {
			t.Errorf("outline leaked config body %q:\n%s", leak, wire)
		}
	}
}

// TestGetOutlineMetadata asserts the outline surfaces a node's freeform metadata
// (element-metadata) ONLY for the eligible nodes that carry it, and omits it
// elsewhere. The seeded document attaches metadata to the root container and one
// block wrapper; the body container and the bare tables carry none. It invokes the
// typed handler directly.
func TestGetOutlineMetadata(t *testing.T) {
	svc := newMetadataService(t)

	res, err := getOutline(context.Background(), svc, getOutlineInput{ID: metadataFixtureID})
	if err != nil {
		t.Fatalf("getOutline: %v", err)
	}

	var out struct {
		Root *decodedOutlineNode `json:"root"`
	}
	remarshal(t, res, &out)
	if out.Root == nil {
		t.Fatalf("root is nil")
	}

	// Index every node by id so eligibility/presence can be asserted per node.
	nodes := map[string]*decodedOutlineNode{}
	var walk func(n *decodedOutlineNode)
	walk = func(n *decodedOutlineNode) {
		if n.ID != "" {
			nodes[n.ID] = n
		}
		for i := range n.Children {
			walk(&n.Children[i])
		}
	}
	walk(out.Root)

	// Root (eligible) carries its metadata.
	root := nodes["root"]
	if root == nil {
		t.Fatalf("root node not in outline")
	}
	if got, want := root.Metadata["owner"], "platform-team"; got != want {
		t.Errorf("root metadata owner = %v, want %v", got, want)
	}

	// The block wrapper (eligible) carries its metadata.
	block := nodes["fruits-block"]
	if block == nil {
		t.Fatalf("fruits-block node not in outline")
	}
	if got, want := block.Metadata["source"], "produce-api"; got != want {
		t.Errorf("fruits-block metadata source = %v, want %v", got, want)
	}

	// Nodes that declared no metadata omit the field entirely (nil decoded map).
	for _, id := range []string{"body", "fruits", "metrics-block", "metrics"} {
		if n := nodes[id]; n != nil && n.Metadata != nil {
			t.Errorf("node %q unexpectedly carries metadata %v, want omitted", id, n.Metadata)
		}
	}

	// The omitempty discipline must hold on the wire: the seeded metadata values are
	// present and no metadata key rides along for a metadata-free node.
	b, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	wire := string(b)
	if !strings.Contains(wire, "platform-team") || !strings.Contains(wire, "produce-api") {
		t.Errorf("outline did not carry the seeded metadata values:\n%s", wire)
	}
}

// TestGetOutlineUnknownIDIsCodedError asserts an unknown id surfaces the store's
// STORAGE_NOT_FOUND coded error verbatim (the handler returns the facade's
// *errors.CodedError unwrapped).
func TestGetOutlineUnknownIDIsCodedError(t *testing.T) {
	svc := newTestService(t)

	_, err := getOutline(context.Background(), svc, getOutlineInput{ID: "does-not-exist"})
	if err == nil {
		t.Fatalf("expected an error for unknown id, got success")
	}
	if !errors.HasCode(err, errors.STORAGE_NOT_FOUND) {
		t.Errorf("error = %v, want it to carry STORAGE_NOT_FOUND", err)
	}
}
