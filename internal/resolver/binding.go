package resolver

// This file implements the E4-S2 ITEM QUERY-BINDING pass: the direct data-flow
// model in which an item references a document-scoped connection by id and
// carries its own query. The pass runs AFTER the instance walk and the
// connection pass, because it needs both the assembled resolved tree (to read
// each item's already-interpolated config) and the resolved connections (to
// validate that a referenced connectionId actually exists).
//
// Query parameters are NOT interpolated here: the E3-S2 interpolation pass
// (variables.Interpolate) already ran over the whole item config — including the
// query object — during resolveInstance, so by the time this pass reads the
// query it carries concrete, typed values, not $var/${} references. This pass
// therefore only validates the connection reference and lifts the binding onto
// the resolved node.
//
// The pass lives in its own file (per file-ownership rules) and is invoked by a
// single call from resolver.go's resolveBytes.

import (
	"strconv"

	"github.com/frankbardon/lattice/errors"
	"github.com/frankbardon/lattice/internal/schema"
)

// bindingConnectionKey and bindingQueryKey are the reserved config keys that
// declare an item's direct data binding. They live in the item's config object
// (schema-validated additively per item type); a query without a connectionId is
// a fail-fast BINDING_INVALID error.
const (
	bindingConnectionKey = "connectionId"
	bindingQueryKey      = "query"
)

// resolveBindings walks the assembled resolved tree and attaches a
// ResolvedBinding to every item that declared a connectionId, validating that
// the id matches one of the document's resolved connections (E4-S2) and that the
// item↔connection result-shape contract holds (E4-S3). It is fail-fast: the
// first offending item stops the walk and is returned as a CodedError naming the
// instance path. Items without a binding are left untouched. g supplies the
// item-type schemas whose expectedResult keyword declares each contract.
func resolveBindings(g *schema.ResolvedGraph, root *ResolvedInstance, conns []*ResolvedConnection) error {
	known := make(map[string]*ResolvedConnection, len(conns))
	for _, c := range conns {
		known[c.ID] = c
	}
	return bindInstance(g, root, "root", known)
}

// bindInstance resolves one node's binding (if any) and recurses into children.
func bindInstance(g *schema.ResolvedGraph, inst *ResolvedInstance, path string, known map[string]*ResolvedConnection) error {
	binding, conn, err := bindingFromConfig(inst.Config, path, known)
	if err != nil {
		return err
	}
	if binding != nil {
		// E4-S3: validate and attach the result-shape contract for the bound item.
		// conn is the resolved connection the binding points at (guaranteed
		// non-nil when binding != nil).
		contract, err := resolveContract(g, inst, conn, path)
		if err != nil {
			return err
		}
		binding.Contract = contract
	}
	inst.Binding = binding

	for i, child := range inst.Children {
		childPath := path + ".children[" + strconv.Itoa(i) + "]"
		if err := bindInstance(g, child, childPath, known); err != nil {
			return err
		}
	}
	return nil
}

// bindingFromConfig extracts a ResolvedBinding from an item's config, returning
// the resolved connection it binds to alongside it. It returns (nil, nil, nil)
// when the item declared no binding. A query declared without a connectionId
// fails fast (BINDING_INVALID); a connectionId that matches no resolved
// connection fails fast (BINDING_CONNECTION_NOT_FOUND).
func bindingFromConfig(cfg map[string]any, path string, known map[string]*ResolvedConnection) (*ResolvedBinding, *ResolvedConnection, error) {
	if cfg == nil {
		return nil, nil, nil
	}
	rawConn, hasConn := cfg[bindingConnectionKey]
	rawQuery, hasQuery := cfg[bindingQueryKey]

	if !hasConn {
		if hasQuery {
			return nil, nil, errors.NewCodedErrorWithDetails(errors.BINDING_INVALID,
				"item declared a query without a connectionId",
				map[string]any{"path": path})
		}
		return nil, nil, nil
	}

	connID, ok := rawConn.(string)
	if !ok || connID == "" {
		return nil, nil, errors.NewCodedErrorWithDetails(errors.BINDING_INVALID,
			"item connectionId must be a non-empty string",
			map[string]any{"path": path})
	}

	conn, found := known[connID]
	if !found {
		return nil, nil, errors.NewCodedErrorWithDetails(errors.BINDING_CONNECTION_NOT_FOUND,
			"item connectionId does not match any declared connection",
			map[string]any{"path": path, "connectionId": connID})
	}

	binding := &ResolvedBinding{ConnectionID: connID}
	if hasQuery && rawQuery != nil {
		query, ok := rawQuery.(map[string]any)
		if !ok {
			return nil, nil, errors.NewCodedErrorWithDetails(errors.BINDING_INVALID,
				"item query must be an object",
				map[string]any{"path": path})
		}
		binding.Query = query
	}
	return binding, conn, nil
}
