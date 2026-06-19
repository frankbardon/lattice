package resolver

// This file implements the E4-S1 CONNECTION PASS: document-scoped data
// connections are decoded, each connection's $ref is resolved to a
// connection-type schema (via the same loader machinery used for item-type
// instance $refs), and each connection's config is validated against that type.
// Connections are declared and validated ONLY — they are NEVER dialed (no live
// fetch this effort). Duplicate connection ids fail fast.
//
// The pass lives in its own file (per file-ownership rules) and is invoked by a
// single call from resolver.go's resolveBytes; the resolved connections are
// attached to the resolved tree root (ResolvedTree.Connections).

import (
	"encoding/json"
	"strconv"

	"github.com/frankbardon/lattice/errors"
	"github.com/frankbardon/lattice/internal/schema"
)

// ResolvedConnection is one document-scoped data connection after resolution: a
// connection instance with its connection-type $ref fully resolved to a
// canonical, versioned identity and its config validated against that type. It
// mirrors ResolvedInstance's shape for items. Connections are declared and
// validated only; this struct never implies a live/dialed connection.
type ResolvedConnection struct {
	// ID is the document-unique connection identifier. Items bind to a
	// connection by this id (later efforts). Always non-empty after resolution.
	ID string `json:"id"`

	// Type is the resolved connection type this connection is an instance of:
	// its canonical identifier plus the parsed name/version. The stable hook
	// downstream code uses to dispatch on connection type (e.g. http vs static).
	Type ResolvedTypeRef `json:"type"`

	// Config is the connection's verbatim, schema-validated configuration object.
	// Its structure is defined by the connection-type's schema; opaque here.
	// Nil when the connection declared no config.
	Config map[string]any `json:"config,omitempty"`

	// SecretRefs is the connection's verbatim secret-reference indirection map,
	// passed through unchanged. Secret values are never inlined; this maps a
	// logical name to an opaque reference token. Nil when none were declared.
	SecretRefs map[string]string `json:"secretRefs,omitempty"`
}

// connectionInstance is the raw decoded form of a document-scoped connection.
// It is decoded from the raw document bytes (rather than added to the shared
// schema.Document type) so the connection pass is self-contained.
type connectionInstance struct {
	ID         string            `json:"id"`
	Ref        string            `json:"$ref"`
	Config     map[string]any    `json:"config,omitempty"`
	SecretRefs map[string]string `json:"secretRefs,omitempty"`
}

// connectionsEnvelope is the minimal slice of the document carrying only the
// document-scoped connections array, used to decode connections without
// disturbing the schema.Document decode path.
type connectionsEnvelope struct {
	Connections []connectionInstance `json:"connections"`
}

// resolveConnections runs the connection pass: it decodes the document's
// connections, resolves and validates each one, and rejects duplicate ids. It
// returns the resolved connections in declaration order, or the first
// CodedError. A document with no connections yields a nil slice (no error).
func (r *Resolver) resolveConnections(g *schema.ResolvedGraph, data []byte, source string) ([]*ResolvedConnection, error) {
	var env connectionsEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, errors.WrapCodedErrorWithDetails(err, errors.CONNECTION_INVALID,
			"failed decoding dashboard connections", map[string]any{"source": source})
	}
	if len(env.Connections) == 0 {
		return nil, nil
	}

	seen := make(map[string]struct{}, len(env.Connections))
	out := make([]*ResolvedConnection, 0, len(env.Connections))
	for i, conn := range env.Connections {
		path := "connections[" + strconv.Itoa(i) + "]"

		if _, dup := seen[conn.ID]; dup {
			return nil, errors.NewCodedErrorWithDetails(errors.CONNECTION_DUPLICATE_ID,
				"duplicate connection id", map[string]any{
					"path": path,
					"id":   conn.ID,
				})
		}
		seen[conn.ID] = struct{}{}

		resolved, err := r.resolveConnection(g, conn, path)
		if err != nil {
			return nil, err
		}
		out = append(out, resolved)
	}
	return out, nil
}

// resolveConnection resolves a single connection's $ref to its connection-type
// schema and validates its config against that schema. Failures fail fast as
// CodedErrors naming the offending connection path.
func (r *Resolver) resolveConnection(g *schema.ResolvedGraph, conn connectionInstance, path string) (*ResolvedConnection, error) {
	if conn.Ref == "" {
		return nil, errors.NewCodedErrorWithDetails(errors.CONNECTION_INVALID,
			"connection is missing a $ref", map[string]any{"path": path, "id": conn.ID})
	}

	// Resolve the connection-type $ref using the SAME loader machinery that
	// resolves item-type instance $refs (catalog by $id, relative roots, inline).
	rt, err := r.loader.ResolveRef(g, conn.Ref)
	if err != nil {
		return nil, errors.WrapCodedErrorWithDetails(err, errors.CONNECTION_TYPE_UNRESOLVED,
			"connection type $ref did not resolve to a known connection type",
			map[string]any{"path": path, "id": conn.ID, "ref": conn.Ref})
	}

	// Validate the connection's config against its connection-type schema. An
	// absent config validates as an empty object so required-field constraints
	// in the connection-type schema still apply.
	resolved, err := rt.Schema.Resolve(nil)
	if err != nil {
		return nil, errors.WrapCodedErrorWithDetails(err, errors.RESOLVE_INTERNAL,
			"failed compiling connection-type schema for validation",
			map[string]any{"path": path, "type": rt.ID})
	}
	var cfg any = conn.Config
	if conn.Config == nil {
		cfg = map[string]any{}
	}
	if err := resolved.Validate(cfg); err != nil {
		return nil, errors.WrapCodedErrorWithDetails(err, errors.CONNECTION_CONFIG_INVALID,
			"connection config failed connection-type schema validation",
			map[string]any{"path": path, "id": conn.ID, "type": rt.ID})
	}

	return &ResolvedConnection{
		ID: conn.ID,
		Type: ResolvedTypeRef{
			Ref:     conn.Ref,
			ID:      rt.ID,
			Name:    rt.Name,
			Version: rt.Version,
		},
		Config:     conn.Config,
		SecretRefs: conn.SecretRefs,
	}, nil
}
