package mcp

import (
	"context"
	"fmt"
	"sort"
	"strings"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/frankbardon/lattice/service"
)

// The get_outline tool registered here is the token-cheap navigation entry point
// (E2-S1): it resolves a dashboard server-side and returns a CONFIG-FREE skeleton
// of the tree plus a document-scope summary and the current revision, so an MCP
// host can locate a target node by id without pulling the whole (config-laden)
// document through context. Like the other tools it calls ONLY the ./service
// facade and surfaces the facade's *errors.CodedError verbatim as a tool error.

func init() {
	registrars = append(registrars, registerGetOutline)
}

// getOutlineInput is the input for get_outline: the document's manifest id.
type getOutlineInput struct {
	// ID is the manifest id of the document to outline (as listed by
	// list_dashboards).
	ID string `json:"id" jsonschema:"the manifest id of the document to outline"`
}

// getOutlineOutput is the structured result of get_outline: the document's
// config-free skeleton, a document-scope summary, and the current revision.
type getOutlineOutput struct {
	// ID echoes the requested manifest id.
	ID string `json:"id" jsonschema:"the requested manifest id"`

	// Revision is the document's current opaque revision token (service.Revision),
	// the value a caller pairs with the eventual write's optimistic-concurrency
	// precondition. Omitted when the wired store does not support revisions.
	Revision string `json:"revision,omitempty" jsonschema:"the document's current opaque revision token, to pass to a later write"`

	// Document is the document-scope summary: variable names, connection ids, and
	// whether a default theme is present — the document-level metadata that does not
	// belong to any single node.
	Document documentScope `json:"document" jsonschema:"document-scope summary: variable names, connection ids, theme presence"`

	// Root is the config-free skeleton of the resolved root node (and, recursively,
	// its children). Never nil for a resolvable document.
	Root *outlineNode `json:"root" jsonschema:"the config-free skeleton of the tree root"`
}

// documentScope is the document-level summary carried in the outline: the names
// of declared variables, the ids of declared connections, and whether a default
// theme is present. It carries NO config or value bodies — names and ids only.
type documentScope struct {
	// Variables are the names of the variables in scope at the document root, sorted.
	// Empty when the document declares no variables.
	Variables []string `json:"variables" jsonschema:"names of variables in scope at the document root"`

	// Connections are the ids of the document-scoped connections, in declaration
	// order. Empty when the document declares none.
	Connections []string `json:"connections" jsonschema:"ids of the document-scoped connections"`

	// Theme reports whether the document declares a default theme. The theme's
	// token values are deliberately omitted — presence only.
	Theme bool `json:"theme" jsonschema:"true when the document declares a default theme"`
}

// outlineNode is one node of the config-free skeleton: the node's id, its short
// type ref, an optional title (only when the node's config exposes one), whether
// it is a container, a compact placement summary, and its children.
//
// Children is typed `any` rather than []*outlineNode on purpose: the MCP SDK's
// reflection-based output-schema generator panics on a Go type that is recursive
// through itself (the same cycle that blocks using resolver.ResolvedInstance as a
// typed output). Each element is in fact an *outlineNode; `any` only relaxes the
// generated schema for the children field so the recursive shape is left
// unconstrained.
type outlineNode struct {
	// ID is the node's stable instance id. Empty only when the node declared none
	// (the resolver permits an id-less node).
	ID string `json:"id,omitempty" jsonschema:"the node's stable instance id"`

	// Type is the node's short resolved type ref (e.g. "container", "table"),
	// falling back to the canonical type id when no short name was parsed.
	Type string `json:"type" jsonschema:"the node's resolved item type (short ref)"`

	// Title is the node's title, included ONLY when the node's config carries a
	// string "title" field; omitted otherwise (never fabricated).
	Title string `json:"title,omitempty" jsonschema:"the node's title, present only when its config declares one"`

	// Container reports whether the node's resolved type is a container (the only
	// type permitted children).
	Container bool `json:"container" jsonschema:"true when the node's type is a container"`

	// Placement is a compact, human-readable summary of the node's placement hints
	// (e.g. "col 2+1, row 1+1"). Omitted when the node declared no placement. It is
	// a SUMMARY, not the verbatim placement config object.
	Placement string `json:"placement,omitempty" jsonschema:"compact summary of the node's grid placement, when placed"`

	// Children are the node's child skeletons (each an *outlineNode), in document
	// order. Omitted for leaf nodes. Typed `any` to avoid the reflective
	// output-schema generator's recursive-type panic.
	Children any `json:"children,omitempty" jsonschema:"the node's child skeletons, in document order"`
}

// registerGetOutline registers the get_outline tool: it resolves the document via
// service.Resolve, projects the resolved tree into a config-free skeleton, adds
// the document-scope summary and the current revision (service.Revision), and
// returns them. An unknown id surfaces the store's STORAGE_NOT_FOUND coded error
// verbatim as a tool error.
func registerGetOutline(s *sdkmcp.Server, svc *service.Service) {
	sdkmcp.AddTool(s, &sdkmcp.Tool{
		Name:        "get_outline",
		Description: "Resolve a dashboard by manifest id and return a config-free skeleton of its tree (per node: id, type, optional title, placement summary, children) plus a document-scope summary (variable names, connection ids, theme presence) and the current revision. Token-cheap navigation: use it to locate a node by id before a targeted read.",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, input getOutlineInput) (*sdkmcp.CallToolResult, getOutlineOutput, error) {
		tree, err := svc.Resolve(input.ID, nil)
		if err != nil {
			// Unknown id surfaces STORAGE_NOT_FOUND verbatim; resolution failures
			// (RESOLVE_*/SCHEMA_*/VAR_*) surface verbatim too.
			return nil, getOutlineOutput{}, err
		}

		out := getOutlineOutput{
			ID: input.ID,
			Document: documentScope{
				Variables:   variableNames(tree),
				Connections: connectionIDs(tree),
				Theme:       tree.DefaultTheme != nil,
			},
			Root: outlineFromInstance(tree.Root),
		}

		// Revision is best-effort: a store that lacks the RevisionedStore capability
		// reports STORAGE_CAPABILITY_UNSUPPORTED. The outline is still useful without
		// a revision, so an unsupported store leaves Revision empty rather than
		// failing the whole tool.
		if rev, rerr := svc.Revision(input.ID); rerr == nil {
			out.Revision = rev
		}

		return nil, out, nil
	})
}

// outlineFromInstance projects a resolved instance into its config-free skeleton,
// recursing into children. It returns nil for a nil instance.
func outlineFromInstance(inst *service.ResolvedInstance) *outlineNode {
	if inst == nil {
		return nil
	}
	node := &outlineNode{
		ID:        inst.ID,
		Type:      shortType(inst.Type),
		Title:     configTitle(inst.Config),
		Container: inst.Container,
		Placement: placementSummary(inst.Placement),
	}
	if len(inst.Children) > 0 {
		children := make([]*outlineNode, 0, len(inst.Children))
		for _, child := range inst.Children {
			children = append(children, outlineFromInstance(child))
		}
		node.Children = children
	}
	return node
}

// shortType returns the short, human type ref for a node — the parsed type name
// when present, else the canonical type id.
func shortType(ref service.ResolvedTypeRef) string {
	if ref.Name != "" {
		return ref.Name
	}
	return ref.ID
}

// configTitle returns the node's title ONLY when its config carries a string
// "title" field; it returns "" otherwise so the caller omits the field rather than
// fabricating one.
func configTitle(config map[string]any) string {
	if config == nil {
		return ""
	}
	if t, ok := config["title"].(string); ok {
		return t
	}
	return ""
}

// placementSummary renders a node's verbatim placement hints into a compact,
// human-readable summary (e.g. "col 2+1, row 1+1"). It returns "" for a node with
// no placement, so the caller omits the field. The summary is intentionally lossy:
// it conveys position WITHOUT echoing the full placement config object.
func placementSummary(placement map[string]any) string {
	if len(placement) == 0 {
		return ""
	}
	col := placementSpan(placement, "colStart", "colSpan")
	row := placementSpan(placement, "rowStart", "rowSpan")

	var parts []string
	if col != "" {
		parts = append(parts, "col "+col)
	}
	if row != "" {
		parts = append(parts, "row "+row)
	}
	if len(parts) > 0 {
		return strings.Join(parts, ", ")
	}
	// Placement present but not in the recognized grid shape: report its keys so the
	// node is still flagged as placed without echoing values.
	keys := make([]string, 0, len(placement))
	for k := range placement {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return "placed (" + strings.Join(keys, ", ") + ")"
}

// placementSpan formats a "<start>+<span>" fragment from the given start/span
// keys, tolerating either present. It returns "" when neither key is present.
func placementSpan(placement map[string]any, startKey, spanKey string) string {
	start, hasStart := placement[startKey]
	span, hasSpan := placement[spanKey]
	switch {
	case hasStart && hasSpan:
		return fmt.Sprintf("%s+%s", numStr(start), numStr(span))
	case hasStart:
		return numStr(start)
	case hasSpan:
		return "?+" + numStr(span)
	default:
		return ""
	}
}

// numStr renders a JSON number (decoded as float64) without a trailing ".0" for
// integers, falling back to the default format for non-numbers.
func numStr(v any) string {
	if f, ok := v.(float64); ok {
		if f == float64(int64(f)) {
			return fmt.Sprintf("%d", int64(f))
		}
		return fmt.Sprintf("%g", f)
	}
	return fmt.Sprintf("%v", v)
}

// variableNames returns the sorted names of the variables in scope at the document
// root (the root instance's resolved variable environment). It returns an empty
// (non-nil) slice when no variables are in scope.
func variableNames(tree *service.ResolvedTree) []string {
	names := make([]string, 0)
	if tree.Root == nil {
		return names
	}
	for name := range tree.Root.VarEnv {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// connectionIDs returns the ids of the document-scoped connections in declaration
// order. It returns an empty (non-nil) slice when none are declared.
func connectionIDs(tree *service.ResolvedTree) []string {
	ids := make([]string, 0, len(tree.Connections))
	for _, c := range tree.Connections {
		if c != nil {
			ids = append(ids, c.ID)
		}
	}
	return ids
}
