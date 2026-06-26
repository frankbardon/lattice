package mcp

import (
	"context"

	"github.com/frankbardon/lattice/service"
)

// get_node is the drill-in read: given a document id and a node's stable instance
// id it returns the STORED JSON subtree for that node (the shape a raw patch
// edits), the node's editable SURFACE (the resolver's per-node configurable
// fields, so the model knows which id-rooted patch paths are valid), and the
// document's current revision. Block-vs-content addressing is resolved
// server-side: when the node is a block wrapper the subtree is the whole block
// (wrapper plus its config/content) and the surface is the CONTENT item's editable
// fields, since a block delegates its knobs to what it wraps.
//
// Like the other tools it calls ONLY the ./service facade (NodeView) and surfaces
// the facade's *errors.CodedError verbatim as a tool error — an unknown document
// id and an unknown node id give DISTINCT coded errors (STORAGE_NOT_FOUND vs
// CHANGESET_TARGET_NOT_FOUND).

// getNodeInput is the input for get_node: the document's manifest id and the
// stable instance id of the node to drill into.
type getNodeInput struct {
	// ID is the manifest id of the document the node lives in (as listed by
	// list_dashboards).
	ID string `json:"id" jsonschema:"the manifest id of the document the node lives in"`

	// NodeID is the stable instance id of the node to read (as listed by
	// get_outline). When it names a block wrapper, the whole block subtree is
	// returned and the surface is the content item's editable fields.
	NodeID string `json:"nodeId" jsonschema:"the stable instance id of the node to read (from get_outline)"`
}

// surfaceField is one entry of get_node's editable surface: a configurable field
// the addressed node exposes, as a flat {key, type} pair. The key is the field's
// id-rooted patch-path tail (nested keys dotted, e.g. "grid.gap"); the type is the
// field's value type. It is a flat, non-recursive shape, so it is typed normally
// (unlike the arbitrary stored subtree, which is typed `any`).
type surfaceField struct {
	// Key is the editable field's path tail relative to the node id — the literal
	// remainder of a valid id-rooted patch pointer ("<nodeId>/<key-as-pointer>").
	// Nested fields are dotted (e.g. "grid.gap").
	Key string `json:"key" jsonschema:"the editable field key (nested keys dotted), the tail of a valid id-rooted patch path"`

	// Type is the field's value type (string, number, integer, boolean, enum,
	// array) — the editor primitive the field needs.
	Type string `json:"type" jsonschema:"the field's value type"`
}

// getNodeOutput is the structured result of get_node: the stored subtree, the
// editable surface, and the current revision.
type getNodeOutput struct {
	// ID echoes the requested manifest id.
	ID string `json:"id" jsonschema:"the requested manifest id"`

	// NodeID echoes the requested node id.
	NodeID string `json:"nodeId" jsonschema:"the requested node id"`

	// Revision is the document's current opaque revision token (service.Revision),
	// the value a caller pairs with the eventual write's optimistic-concurrency
	// precondition. Omitted when the wired store does not support revisions.
	Revision string `json:"revision,omitempty" jsonschema:"the document's current opaque revision token, to pass to a later write"`

	// Subtree is the STORED JSON subtree for the addressed node — the exact shape a
	// raw id-rooted patch edits. For a block it is the whole block subtree (wrapper
	// plus config/content). It is typed `any` so the reflected output-schema leaves
	// it unconstrained: the subtree's shape is governed by the item/document schemas,
	// not this tool's contract (and the reflector cannot represent the arbitrary,
	// recursive document shape).
	Subtree any `json:"subtree" jsonschema:"the stored JSON subtree for the node (the shape a patch edits)"`

	// Surface is the node's editable surface as a flat {key, type} list. For a block
	// it is the CONTENT item's editable fields. Empty when the node declares no
	// configurable surface. NOTE: this gates only FIELD edits; structural edits
	// (add/remove/move children) are NOT surface-listed — use get_outline for those.
	Surface []surfaceField `json:"surface" jsonschema:"the node's editable field surface as a flat key+type list; empty when none"`
}

// getNodeDescription is the get_node tool description, kept identical to the legacy
// registration so downstream catalog text (get_manifest) holds parity.
const getNodeDescription = "Drill into one node by document id + node id. Returns the STORED JSON subtree for that node (the exact shape a raw id-rooted patch edits), the node's editable field surface as a flat key+type list (so you know which field paths are valid to patch), and the current revision. Block addressing: naming a block returns the whole block subtree (wrapper + config/content) and surfaces the CONTENT item's editable fields. The surface gates FIELD edits only — structural edits (add/remove/move children) are NOT surface-gated; use get_outline to plan those."

// getNode calls service.NodeView to fetch the addressed node's stored subtree,
// editable surface, and current revision, and projects the surface into a flat
// {key, type} list. An unknown document id surfaces STORAGE_NOT_FOUND and an
// unknown node id surfaces CHANGESET_TARGET_NOT_FOUND — distinct coded errors —
// verbatim as a tool error.
func getNode(_ context.Context, svc *service.Service, in getNodeInput) (getNodeOutput, error) {
	view, err := svc.NodeView(in.ID, in.NodeID)
	if err != nil {
		// Unknown document id (STORAGE_NOT_FOUND) and unknown node id
		// (CHANGESET_TARGET_NOT_FOUND) surface verbatim as distinct tool errors;
		// resolution failures (RESOLVE_*/SCHEMA_*/VAR_*) surface verbatim too.
		return getNodeOutput{}, err
	}

	out := getNodeOutput{
		ID:       in.ID,
		NodeID:   in.NodeID,
		Revision: view.Revision,
		Subtree:  view.Subtree,
		Surface:  surfaceList(view.Surface),
	}
	return out, nil
}

// surfaceList projects the resolver's per-node configurable surface into the flat
// {key, type} list the tool returns. The key is the field's id-rooted patch-path
// tail (ConfigurableField.Field, nested keys dotted); the type is the field's
// value type. It returns a non-nil (possibly empty) slice so the output always
// carries a surface array.
func surfaceList(fields []service.ConfigurableField) []surfaceField {
	out := make([]surfaceField, 0, len(fields))
	for _, f := range fields {
		out = append(out, surfaceField{Key: f.Field, Type: string(f.Type)})
	}
	return out
}
