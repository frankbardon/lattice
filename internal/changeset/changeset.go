// Package changeset is the apply-layer foundation for the patch-write pipeline.
// A changeset is an RFC 6902 JSON Patch document — an ordered array of
// operations ({op, path, value?, from?}) — whose pointers are ID-ROOTED: the
// leading pointer segment is an item's stable `id` or a reserved `$`-scope
// keyword (`$manifest`/`$variables`/`$connections`/`$theme`/`$root`), and the
// remainder is literal RFC 6901. This package (a) PARSES a changeset document
// into a typed model, and (b) TRANSLATES each id-rooted pointer into a physical
// RFC 6901 pointer against the decoded document tree (translate.go).
//
// Why id-rooted pointers, and why translate against the DECODED document: the
// document on disk is loaded fully-decoded into a generic JSON tree
// (map[string]any) — a patch cannot be applied to raw bytes. The apply path
// (E1-S2) is: stored bytes -> map[string]any -> translate id-rooted pointer +
// apply RFC 6902 -> canonical re-marshal -> re-resolve. This package owns the
// first two contracts; the standard RFC 6902 applier (chosen below) consumes the
// translated physical pointers in E1-S2.
//
// APPLIER CHOICE (decided here for E1-S2): the application step will use
// github.com/evanphx/json-patch/v5 — the de-facto RFC 6902 applier for Go. It
// applies a patch document (as []byte, or via DecodePatch + Apply over JSON
// bytes) implementing all six ops (add/remove/replace/move/copy/test) with
// correct array-index/`-`/escaping/atomicity semantics, including `test` (needed
// for the revision-precondition story). This package emits a standard physical
// RFC 6901 pointer string per op, which is exactly that library's input dialect,
// so E1-S2 rewrites each op's `path`/`from` with the translated pointer and hands
// the resulting patch document to json-patch. The dependency is added by E1-S2
// when the import first lands (a dep with no importing code is stripped by
// `go mod tidy`, so it is not carried in go.mod by this story). We do NOT
// hand-roll the applier: the edge cases (array `-`, pointer escaping, atomic
// rollback) are exactly what the library already handles correctly.
package changeset

import (
	"encoding/json"

	"github.com/frankbardon/lattice/errors"
)

// Operation kinds, per RFC 6902 section 4.
const (
	OpAdd     = "add"
	OpRemove  = "remove"
	OpReplace = "replace"
	OpMove    = "move"
	OpCopy    = "copy"
	OpTest    = "test"
)

// Operation is a single RFC 6902 JSON Patch operation as written in a changeset.
// Its Path (and From, for move/copy) are ID-ROOTED pointers — the leading
// segment is an item id or `$`-scope; Translate (translate.go) maps them to
// physical RFC 6901 pointers. Value is preserved as a raw JSON message so the
// applier sees the author's exact value verbatim (it is opaque to this package);
// HasValue/HasFrom record whether the member was present in the source object,
// distinguishing an absent member from an explicit JSON null.
type Operation struct {
	// Op is the operation kind (one of the Op* constants).
	Op string
	// Path is the id-rooted RFC 6901-shaped pointer the op targets.
	Path string
	// From is the id-rooted source pointer for move/copy; empty otherwise.
	From string
	// HasFrom records whether the `from` member was present in the source.
	HasFrom bool
	// Value is the op's value member, preserved verbatim as raw JSON. Nil when
	// the member was absent (see HasValue).
	Value json.RawMessage
	// HasValue records whether the `value` member was present in the source,
	// distinguishing an absent value from an explicit JSON null.
	HasValue bool
}

// Changeset is a parsed RFC 6902 JSON Patch document: an ordered list of
// operations. Order is significant (RFC 6902 applies operations sequentially) and
// is preserved as authored.
type Changeset struct {
	Ops []Operation
}

// rawOp is the wire shape of one operation object, decoded leniently so this
// package — not encoding/json's zero values — decides which members are present
// and well-formed. Pointers distinguish an absent member from a present-but-null
// or present-but-typed one.
type rawOp struct {
	Op    *string         `json:"op"`
	Path  *string         `json:"path"`
	From  *string         `json:"from"`
	Value json.RawMessage `json:"value"`
}

// opNeedsValue reports whether an operation kind requires a `value` member
// (RFC 6902: add/replace/test carry a value; remove/move/copy do not).
func opNeedsValue(op string) bool {
	switch op {
	case OpAdd, OpReplace, OpTest:
		return true
	default:
		return false
	}
}

// opNeedsFrom reports whether an operation kind requires a `from` member
// (RFC 6902: move/copy carry a from; the others do not).
func opNeedsFrom(op string) bool {
	return op == OpMove || op == OpCopy
}

// knownOp reports whether op is one of the six RFC 6902 operation kinds.
func knownOp(op string) bool {
	switch op {
	case OpAdd, OpRemove, OpReplace, OpMove, OpCopy, OpTest:
		return true
	default:
		return false
	}
}

// Parse decodes a changeset document (an RFC 6902 JSON Patch array) into a typed
// Changeset. It validates structure ONLY — that the document is a JSON array of
// operation objects and that each op carries the members its kind requires (a
// string `op` naming one of the six kinds, a string `path`, a `value` for
// add/replace/test, a `from` for move/copy). It does NOT translate or resolve
// pointers (that is Translate) nor enforce the configurable-surface guardrail
// (later stories). Any structural defect fails fast with CHANGESET_INVALID,
// naming the offending operation index. An empty array is a valid (no-op)
// changeset.
func Parse(data []byte) (*Changeset, error) {
	var raws []rawOp
	if err := json.Unmarshal(data, &raws); err != nil {
		return nil, errors.WrapCodedError(err, errors.CHANGESET_INVALID,
			"changeset is not a valid JSON Patch array of operation objects")
	}

	ops := make([]Operation, 0, len(raws))
	for i, raw := range raws {
		op, err := parseOp(i, raw)
		if err != nil {
			return nil, err
		}
		ops = append(ops, op)
	}
	return &Changeset{Ops: ops}, nil
}

// parseOp validates and normalizes one wire operation object at index i.
func parseOp(i int, raw rawOp) (Operation, error) {
	if raw.Op == nil {
		return Operation{}, errors.NewCodedErrorWithDetails(errors.CHANGESET_INVALID,
			"changeset operation is missing its required string `op` member",
			map[string]any{"index": i})
	}
	if !knownOp(*raw.Op) {
		return Operation{}, errors.NewCodedErrorWithDetails(errors.CHANGESET_INVALID,
			"changeset operation names an unknown op (expected add/remove/replace/move/copy/test)",
			map[string]any{"index": i, "op": *raw.Op})
	}
	if raw.Path == nil {
		return Operation{}, errors.NewCodedErrorWithDetails(errors.CHANGESET_INVALID,
			"changeset operation is missing its required string `path` member",
			map[string]any{"index": i, "op": *raw.Op})
	}

	hasValue := raw.Value != nil
	if opNeedsValue(*raw.Op) && !hasValue {
		return Operation{}, errors.NewCodedErrorWithDetails(errors.CHANGESET_INVALID,
			"changeset operation requires a `value` member but none is present",
			map[string]any{"index": i, "op": *raw.Op})
	}

	from := ""
	hasFrom := raw.From != nil
	if hasFrom {
		from = *raw.From
	}
	if opNeedsFrom(*raw.Op) && !hasFrom {
		return Operation{}, errors.NewCodedErrorWithDetails(errors.CHANGESET_INVALID,
			"changeset operation requires a `from` member but none is present",
			map[string]any{"index": i, "op": *raw.Op})
	}

	return Operation{
		Op:       *raw.Op,
		Path:     *raw.Path,
		From:     from,
		HasFrom:  hasFrom,
		Value:    raw.Value,
		HasValue: hasValue,
	}, nil
}
