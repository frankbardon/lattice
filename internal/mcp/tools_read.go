package mcp

import (
	"context"
	"encoding/json"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/frankbardon/lattice/service"
)

// The two read tools registered here — list_dashboards and get_document — are the
// discover-and-read entry points an MCP host uses to enumerate stored dashboards
// and pull a whole document (raw and, optionally, resolved). They call ONLY the
// ./service facade and surface the facade's *errors.CodedError verbatim as MCP
// tool errors (returning the error from a ToolHandlerFor packs it into the result
// Content with IsError set; see the SDK's ToolHandlerFor contract), never as a
// flattened plain string.

func init() {
	registrars = append(registrars, registerListDashboards, registerGetDocument)
}

// listDashboardsInput is the input for list_dashboards: it takes no arguments.
// AddTool still requires an object-typed input schema, so this is an empty
// struct.
type listDashboardsInput struct{}

// dashboardSummary is one entry in the list_dashboards output: a stored
// document's manifest id and, when cheaply readable from the manifest, its title.
type dashboardSummary struct {
	// ID is the document's manifest.id — the addressing key Load/Resolve accept.
	ID string `json:"id" jsonschema:"the document's manifest id (the key get_document accepts)"`

	// Title is the document's manifest.title, omitted when the manifest carries
	// none. It is read from the raw stored bytes without resolving the document.
	Title string `json:"title,omitempty" jsonschema:"the document's manifest title, when present"`
}

// listDashboardsOutput is the structured result of list_dashboards: the stored
// documents in store-listing order.
type listDashboardsOutput struct {
	// Dashboards are the stored documents' summaries (id + optional title).
	Dashboards []dashboardSummary `json:"dashboards" jsonschema:"the stored dashboards, each with its manifest id and optional title"`
}

// registerListDashboards registers the list_dashboards tool: it enumerates the
// stored document ids via service.List and, for each, reads the manifest title
// cheaply from the raw stored bytes (a manifest-only unmarshal — no resolution).
// A document whose bytes cannot be read or whose manifest cannot be parsed for a
// title is still listed by id with an empty title; the listing is best-effort on
// the title and authoritative on the id set.
func registerListDashboards(s *sdkmcp.Server, svc *service.Service) {
	sdkmcp.AddTool(s, &sdkmcp.Tool{
		Name:        "list_dashboards",
		Description: "List the stored dashboard documents, each with its manifest id and (when available) title. The id is the key get_document accepts.",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, _ listDashboardsInput) (*sdkmcp.CallToolResult, listDashboardsOutput, error) {
		ids, err := svc.List()
		if err != nil {
			// Surface the store's *errors.CodedError verbatim as a tool error.
			return nil, listDashboardsOutput{}, err
		}

		out := listDashboardsOutput{Dashboards: make([]dashboardSummary, 0, len(ids))}
		for _, id := range ids {
			summary := dashboardSummary{ID: id}
			// Best-effort title: read the raw bytes and unmarshal only the
			// manifest. A read/parse failure does not drop the document from the
			// listing — the id is authoritative, the title is opportunistic.
			if b, lerr := svc.Load(id); lerr == nil {
				summary.Title = manifestTitle(b)
			}
			out.Dashboards = append(out.Dashboards, summary)
		}
		return nil, out, nil
	})
}

// manifestTitle extracts manifest.title from raw document bytes without running
// resolution. It returns "" when the bytes are not the expected shape or carry no
// string title — the caller treats a missing title as simply absent.
func manifestTitle(b []byte) string {
	var doc struct {
		Manifest struct {
			Title string `json:"title"`
		} `json:"manifest"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		return ""
	}
	return doc.Manifest.Title
}

// getDocumentInput is the input for get_document: the document's manifest id and
// an optional flag requesting the resolved tree alongside the raw bytes.
type getDocumentInput struct {
	// ID is the manifest id of the document to fetch (as listed by
	// list_dashboards).
	ID string `json:"id" jsonschema:"the manifest id of the document to fetch"`

	// Resolved, when true, additionally runs the two-pass resolver and returns the
	// resolved tree. When false (the default), only the raw stored bytes are
	// returned.
	Resolved bool `json:"resolved,omitempty" jsonschema:"when true, also return the resolved tree (runs the resolver); defaults to false"`
}

// getDocumentOutput is the structured result of get_document: the raw stored
// document and, when requested, its resolved tree.
type getDocumentOutput struct {
	// ID echoes the requested manifest id.
	ID string `json:"id" jsonschema:"the requested manifest id"`

	// Document is the stored document as a decoded JSON value (the bytes returned
	// by service.Load, unmarshaled so the result is structured rather than a
	// string). It is typed `any` so the reflective output-schema leaves it
	// unconstrained — the document's shape is governed by the dashboard schema, not
	// this tool's contract.
	Document any `json:"document" jsonschema:"the stored document as JSON"`

	// Resolved is the resolved tree as a decoded JSON value, present only when the
	// input requested it. It is typed `any` rather than the typed
	// service.ResolvedTree both because the resolved tree is recursive (a node's
	// children are nodes), which the reflective output-schema generator cannot
	// represent, and so the output schema leaves it unconstrained. The value is the
	// resolver's structured output verbatim.
	Resolved any `json:"resolved,omitempty" jsonschema:"the resolved tree as JSON, present only when the resolved input flag is set"`
}

// registerGetDocument registers the get_document tool: the whole-document escape
// hatch. It returns the raw stored bytes via service.Load and, when input.Resolved
// is set, the resolved tree via service.Resolve(id, nil). An unknown id surfaces
// the store's STORAGE_NOT_FOUND *errors.CodedError verbatim as a tool error.
func registerGetDocument(s *sdkmcp.Server, svc *service.Service) {
	sdkmcp.AddTool(s, &sdkmcp.Tool{
		Name:        "get_document",
		Description: "Fetch a whole dashboard document by manifest id. Returns the raw stored JSON and, when resolved is true, the resolved tree. This is the escape hatch — prefer the slicing tools for targeted reads.",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, input getDocumentInput) (*sdkmcp.CallToolResult, getDocumentOutput, error) {
		raw, err := svc.Load(input.ID)
		if err != nil {
			// Unknown id surfaces STORAGE_NOT_FOUND verbatim as a tool error.
			return nil, getDocumentOutput{}, err
		}

		var document any
		if err := json.Unmarshal(raw, &document); err != nil {
			return nil, getDocumentOutput{}, err
		}
		out := getDocumentOutput{
			ID:       input.ID,
			Document: document,
		}

		if input.Resolved {
			tree, rerr := svc.Resolve(input.ID, nil)
			if rerr != nil {
				// Resolution failures (RESOLVE_*/SCHEMA_*/VAR_*) surface verbatim.
				return nil, getDocumentOutput{}, rerr
			}
			out.Resolved = tree
		}

		return nil, out, nil
	})
}
