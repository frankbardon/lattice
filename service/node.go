package service

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/frankbardon/lattice/errors"
	"github.com/frankbardon/lattice/internal/changeset"
)

// blockTypeName is the resolved item-type name of a block WRAPPER. A block is the
// patch/target anchor downstream tools address: its single inner content item
// lives physically at the wrapper's /config/content (a config field, not a child
// slot) and is modeled as the block node's single child in the resolved tree. The
// node view surfaces the CONTENT item's editable fields when the addressed node is
// a block, so it detects a block by this resolved type name (the same signal the
// resolver grammar uses). See internal/resolver block handling.
const blockTypeName = "block"

// NodeView is the targeted-read result of NodeView: the STORED JSON subtree for a
// single addressed node (the shape a changeset patches), its editable surface, and
// the document's current revision. It is the value the get_node MCP tool returns.
//
// Subtree is the decoded stored JSON for the node — the wrapper-and-content
// subtree when the node is a block. Surface is the editable field set (the
// resolver's per-node ConfigurableField list); for a block it is the CONTENT
// item's surface, since the block delegates its editable knobs to what it wraps.
// Revision is the store's current opaque token (empty when the store lacks the
// RevisionedStore capability).
type NodeView struct {
	// Subtree is the STORED JSON subtree for the addressed node, decoded into a
	// generic JSON value. For a block it is the WHOLE block subtree (wrapper plus
	// its config/content). It is typed `any` so the surface stays unconstrained —
	// the subtree's shape is governed by the document/item schemas, not this type.
	Subtree any

	// Surface is the addressed node's editable surface — the resolver's per-node
	// []ConfigurableField (each entry's Field is the id-rooted patch path's tail,
	// nested keys dotted). When the node is a block, this is the CONTENT item's
	// surface. Empty when the node (or its content) declares no configurable
	// surface. Structural edits (add/remove/move children) are NOT surface-gated.
	Surface []ConfigurableField

	// Revision is the document's current opaque revision token (the value a later
	// write pairs with WithExpectedRevision). Empty when the wired store does not
	// support revisions.
	Revision string
}

// NodeView returns the stored subtree, editable surface, and current revision for
// a single node addressed by its stable instance id within the document addressed
// by id. It is the facade primitive behind the get_node MCP tool — the targeted
// read that makes a raw, id-rooted patch authorable without loading the whole
// document.
//
// It locates the stored subtree by reusing the changeset id→physical-pointer
// index (internal/changeset.NewTranslator): the translator indexes every
// id-carrying instance — including a block's inner content at /config/content — to
// its physical RFC 6901 pointer, and the subtree is read out of the decoded
// document at that pointer. When nodeId names a BLOCK wrapper, the returned subtree
// is the whole block (wrapper plus config/content) and the surface is the CONTENT
// item's editable fields, since a block delegates its editable knobs to what it
// wraps.
//
// Errors are DISTINCT and surface verbatim: an unknown document id is the store's
// STORAGE_NOT_FOUND; an unknown nodeId is CHANGESET_TARGET_NOT_FOUND (the
// translator's not-found code for a leading id matching no item). Resolution
// failures (RESOLVE_*/SCHEMA_*/VAR_*) propagate verbatim from the resolver.
func (s *Service) NodeView(id, nodeID string) (*NodeView, error) {
	raw, err := s.store.Load(id)
	if err != nil {
		// Unknown document id surfaces STORAGE_NOT_FOUND verbatim.
		return nil, err
	}

	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, errors.NewCodedErrorWithDetails(errors.STORAGE_INVALID,
			"stored document is not a JSON object",
			map[string]any{"id": id})
	}

	// Locate the node's physical pointer by reusing the changeset id index (which
	// also indexes a block's inner content at /config/content). An unknown nodeId
	// surfaces CHANGESET_TARGET_NOT_FOUND, DISTINCT from the unknown-id case above.
	pointer, err := changeset.NewTranslator(doc).Translate("/"+nodeID, 0)
	if err != nil {
		return nil, err
	}

	subtree, err := valueAtPointer(doc, pointer)
	if err != nil {
		return nil, err
	}

	view := &NodeView{Subtree: subtree}

	// Resolve the document so each node's validated editable surface is available,
	// then attach the addressed node's surface — or, for a block, its content's.
	tree, err := s.resolver.ResolveBytesWithValues(raw, id, nil)
	if err != nil {
		return nil, err
	}
	view.Surface = surfaceForNode(tree.Root, nodeID)

	// Revision is best-effort: a store lacking the RevisionedStore capability
	// reports STORAGE_CAPABILITY_UNSUPPORTED. The node view is still useful without
	// a revision, so an unsupported store leaves Revision empty.
	if rev, rerr := s.Revision(id); rerr == nil {
		view.Revision = rev
	}

	return view, nil
}

// surfaceForNode returns the editable surface for the node with the given id
// within the resolved tree rooted at root. For a BLOCK wrapper it returns the
// surface of the block's single inner content node (the block delegates its
// editable knobs to what it wraps); for any other node it returns the node's own
// Surface. It returns nil when no node with the id is found or the node (or its
// content) declares no surface.
func surfaceForNode(root *ResolvedInstance, id string) []ConfigurableField {
	inst := findInstance(root, id)
	if inst == nil {
		return nil
	}
	if inst.Type.Name == blockTypeName && len(inst.Children) == 1 && inst.Children[0] != nil {
		// A block's inner content is its single child in the resolved tree; surface
		// the CONTENT item's editable fields.
		return inst.Children[0].Surface
	}
	return inst.Surface
}

// findInstance returns the first node with the given id in a pre-order walk of the
// resolved tree rooted at inst, or nil when none carries it. A block's inner
// content is reachable as the block node's single child, so an id naming inner
// content is found by this walk too.
func findInstance(inst *ResolvedInstance, id string) *ResolvedInstance {
	if inst == nil {
		return nil
	}
	if inst.ID == id {
		return inst
	}
	for _, child := range inst.Children {
		if found := findInstance(child, id); found != nil {
			return found
		}
	}
	return nil
}

// valueAtPointer reads the JSON value at a physical RFC 6901 pointer within a
// decoded document tree. The pointer is the translator's output — a well-formed,
// "/"-rooted physical pointer — so this walk handles object keys and numeric array
// indices and reports CHANGESET_TARGET_NOT_FOUND if any segment does not resolve
// (a defensive guard; the translator only emits pointers to indexed instances).
func valueAtPointer(doc map[string]any, pointer string) (any, error) {
	if pointer == "" {
		return doc, nil
	}
	var cur any = doc
	for _, seg := range strings.Split(pointer[1:], "/") {
		// Unescape RFC 6901: "~1" -> "/", "~0" -> "~" (order matters).
		seg = strings.ReplaceAll(seg, "~1", "/")
		seg = strings.ReplaceAll(seg, "~0", "~")
		switch node := cur.(type) {
		case map[string]any:
			next, ok := node[seg]
			if !ok {
				return nil, pointerNotFound(pointer)
			}
			cur = next
		case []any:
			idx, err := strconv.Atoi(seg)
			if err != nil || idx < 0 || idx >= len(node) {
				return nil, pointerNotFound(pointer)
			}
			cur = node[idx]
		default:
			return nil, pointerNotFound(pointer)
		}
	}
	return cur, nil
}

// pointerNotFound builds the CHANGESET_TARGET_NOT_FOUND error for a physical
// pointer that does not resolve to a value in the document.
func pointerNotFound(pointer string) error {
	return errors.NewCodedErrorWithDetails(errors.CHANGESET_TARGET_NOT_FOUND,
		"node subtree pointer does not resolve to a value in the document",
		map[string]any{"pointer": pointer})
}
