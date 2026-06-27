package mcp

// Typed-handler + descriptor tests for the two read tools (E2-S5) — list_dashboards
// and get_document — ported from the legacy internal/mcp/tools_read_test.go but
// driven WITHOUT a server: the typed handlers are invoked directly, and each
// descriptor's erased Invoke is exercised off the Tools() catalog. The assertions
// preserve the legacy behavior: list_dashboards returns the seeded id with its
// manifest title; get_document returns the raw stored bytes (and the resolved tree
// only with resolved=true), surfaces metadata round-trip, and turns an unknown id
// into the store's STORAGE_NOT_FOUND coded error verbatim.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/frankbardon/lattice/errors"
)

// TestReadToolsRegistered asserts both read tools are present in the Tools()
// catalog with reflection-generated input+output schemas and the legacy
// descriptions — i.e. NewTool reflected the (any-typed, recursive) document/resolved
// fields without panicking.
func TestReadToolsRegistered(t *testing.T) {
	for _, tc := range []struct {
		name string
		desc string
	}{
		{"lattice_list_dashboards", listDashboardsDescription},
		{"lattice_get_document", getDocumentDescription},
	} {
		d := findDescriptor(t, tc.name)
		if d.Description != tc.desc {
			t.Errorf("%s description mismatch:\n got %q\nwant %q", tc.name, d.Description, tc.desc)
		}
		if len(d.InputSchema) == 0 {
			t.Errorf("%s has empty InputSchema (expected reflection-generated)", tc.name)
		}
		if len(d.OutputSchema) == 0 {
			t.Errorf("%s has empty OutputSchema (expected reflection-generated)", tc.name)
		}
	}
}

// TestListDashboards asserts list_dashboards returns the seeded id with its manifest
// title. It drives the descriptor's erased Invoke (no args) so the wire shape a
// transport adapter dispatches is exercised end to end.
func TestListDashboards(t *testing.T) {
	svc := newTestService(t)
	d := findDescriptor(t, "lattice_list_dashboards")

	raw, err := d.Invoke(context.Background(), svc, nil)
	if err != nil {
		t.Fatalf("Invoke list_dashboards: %v", err)
	}

	var out struct {
		Dashboards []struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		} `json:"dashboards"`
	}
	remarshal(t, raw, &out)

	if len(out.Dashboards) != 1 {
		t.Fatalf("dashboards = %d, want 1", len(out.Dashboards))
	}
	if out.Dashboards[0].ID != fixtureID {
		t.Errorf("id = %q, want %q", out.Dashboards[0].ID, fixtureID)
	}
	if out.Dashboards[0].Title == "" {
		t.Errorf("title is empty, want the manifest title")
	}
}

// TestGetDocumentRaw asserts get_document returns the raw stored document and,
// without the resolved flag, no resolved tree. It drives the descriptor's erased
// Invoke.
func TestGetDocumentRaw(t *testing.T) {
	svc := newTestService(t)
	d := findDescriptor(t, "lattice_get_document")

	raw, err := d.Invoke(context.Background(), svc, json.RawMessage(`{"id":"`+fixtureID+`"}`))
	if err != nil {
		t.Fatalf("Invoke get_document: %v", err)
	}

	var out struct {
		ID       string          `json:"id"`
		Document json.RawMessage `json:"document"`
		Resolved json.RawMessage `json:"resolved"`
	}
	remarshal(t, raw, &out)

	if out.ID != fixtureID {
		t.Errorf("id = %q, want %q", out.ID, fixtureID)
	}
	if len(out.Document) == 0 {
		t.Errorf("document is empty, want raw stored bytes")
	}
	if len(out.Resolved) != 0 {
		t.Errorf("resolved present without the resolved flag: %s", out.Resolved)
	}
}

// TestGetDocumentResolved asserts get_document with resolved=true returns the
// resolved tree alongside the raw document. It invokes the typed handler directly.
func TestGetDocumentResolved(t *testing.T) {
	svc := newTestService(t)

	res, err := getDocument(context.Background(), svc, getDocumentInput{ID: fixtureID, Resolved: true})
	if err != nil {
		t.Fatalf("getDocument(resolved): %v", err)
	}

	var out struct {
		Resolved *struct {
			Manifest map[string]any `json:"manifest"`
			Root     map[string]any `json:"root"`
		} `json:"resolved"`
	}
	remarshal(t, res, &out)

	if out.Resolved == nil {
		t.Fatalf("resolved tree absent with resolved=true")
	}
	if out.Resolved.Root == nil {
		t.Errorf("resolved tree has no root")
	}
}

// TestGetDocumentMetadataRoundTrip asserts get_document's RAW output carries the
// stored element metadata through unchanged: the bytes a host reads back are the
// bytes that were stored, so the metadata rides along for free. It re-parses the
// returned raw document and confirms the seeded root and block metadata are present
// verbatim, and that the resolved tree (with resolved=true) carries them too.
func TestGetDocumentMetadataRoundTrip(t *testing.T) {
	svc := newMetadataService(t)

	res, err := getDocument(context.Background(), svc, getDocumentInput{ID: metadataFixtureID, Resolved: true})
	if err != nil {
		t.Fatalf("getDocument(resolved): %v", err)
	}

	var out struct {
		Document json.RawMessage `json:"document"`
		Resolved *struct {
			Root map[string]any `json:"root"`
		} `json:"resolved"`
	}
	remarshal(t, res, &out)

	// Raw round-trip: the returned document re-parses to the stored metadata.
	var doc struct {
		Root struct {
			Metadata map[string]any    `json:"metadata"`
			Children []json.RawMessage `json:"children"`
		} `json:"root"`
	}
	if err := json.Unmarshal(out.Document, &doc); err != nil {
		t.Fatalf("unmarshal returned raw document: %v", err)
	}
	if got, want := doc.Root.Metadata["owner"], "platform-team"; got != want {
		t.Errorf("raw root metadata owner = %v, want %v", got, want)
	}
	// The block's metadata must survive verbatim too (two levels down under body);
	// confirm via a substring of the raw bytes.
	if !strings.Contains(string(out.Document), "produce-api") {
		t.Errorf("raw document dropped the block metadata value %q:\n%s", "produce-api", out.Document)
	}

	// Resolved tree carries the root metadata too.
	if out.Resolved == nil {
		t.Fatalf("resolved tree absent with resolved=true")
	}
	rootMeta, _ := out.Resolved.Root["metadata"].(map[string]any)
	if rootMeta == nil || rootMeta["owner"] != "platform-team" {
		t.Errorf("resolved root metadata = %v, want owner=platform-team", out.Resolved.Root["metadata"])
	}
}

// TestGetDocumentUnknownIDIsCodedError asserts an unknown id surfaces the store's
// STORAGE_NOT_FOUND coded error verbatim (the handler returns the facade's
// *errors.CodedError unwrapped), and that the erased Invoke path surfaces it too.
func TestGetDocumentUnknownIDIsCodedError(t *testing.T) {
	svc := newTestService(t)

	_, err := getDocument(context.Background(), svc, getDocumentInput{ID: "does-not-exist"})
	if err == nil {
		t.Fatalf("expected an error for unknown id, got success")
	}
	if !errors.HasCode(err, errors.STORAGE_NOT_FOUND) {
		t.Errorf("error = %v, want it to carry STORAGE_NOT_FOUND", err)
	}

	// The erased Invoke must surface the same coded error verbatim (not flattened).
	d := findDescriptor(t, "lattice_get_document")
	_, ierr := d.Invoke(context.Background(), svc, json.RawMessage(`{"id":"does-not-exist"}`))
	if !errors.HasCode(ierr, errors.STORAGE_NOT_FOUND) {
		t.Errorf("Invoke error = %v, want it to carry STORAGE_NOT_FOUND verbatim", ierr)
	}
}
