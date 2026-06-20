package changeset

// This file owns ID-ROOTED POINTER TRANSLATION: mapping a changeset pointer
// (whose leading segment is an item `id` or a reserved `$`-scope keyword) to a
// PHYSICAL RFC 6901 pointer into the DECODED document tree (map[string]any).
//
// Translation works against the decoded document, NOT the resolver's resolved
// tree. The resolver's id-index (internal/resolver/configurator.go buildIDIndex +
// config_override.go findByID) yields LOGICAL resolved-tree paths
// ("root.children[2]"), which differ from the document's PHYSICAL layout in two
// ways that matter for a patch: (1) a block wrapper's inner content is a separate
// node in the resolved tree but lives physically at the wrapper's
// `/config/content` (a config field, not a `/children/N` slot); (2) physical
// pointers must be escaped/segmented per RFC 6901. So this package re-walks the
// decoded tree to build a physical id index, mirroring the resolver's pre-order,
// last-wins indexing discipline rather than reusing the resolved-tree walker
// (which would address the wrong physical locations). The id-rooted routing rules
// — `$`-prefix recognized before id lookup, the closed reserved-scope set,
// unknown `$`-scope reusing CONFIGURATOR_TARGET_SCOPE_UNKNOWN — are kept in step
// with the configurator pass.

import (
	"strconv"
	"strings"

	"github.com/frankbardon/lattice/errors"
)

// reservedScopePrefix marks a changeset pointer's leading segment as a RESERVED,
// document-level scope keyword rather than an item instance id, exactly as the
// configurator pass's reservedTargetPrefix does (internal/resolver/configurator.go).
// A leading segment beginning with this sigil is ALWAYS routed to a document scope
// and is NEVER looked up in the id index, so a reserved keyword can never collide
// with an item id.
const reservedScopePrefix = "$"

// reservedScopeBase maps each reserved `$`-scope keyword to the PHYSICAL RFC 6901
// base pointer of the document member it addresses. The set is closed (mirrors the
// configurator pass's reservedTargets); a `$`-prefixed leading segment outside it
// fails fast with CONFIGURATOR_TARGET_SCOPE_UNKNOWN. The bases are the document's
// top-level members: the manifest object, the variable-declaration array, the
// connection array, the default-theme object, and the root instance.
var reservedScopeBase = map[string]string{
	"$manifest":    "/manifest",
	"$variables":   "/variables",
	"$connections": "/connections",
	"$theme":       "/theme",
	"$root":        "/root",
}

// instanceContentKey is the block wrapper config field holding its single inner
// content item (schemas/items/block.schema.json: config.content). Block inner
// content is a CONFIG FIELD, not a `/children/N` child slot, so the id walk
// descends into it physically at `/config/content`. Only this config key carries a
// nested instance; all other config is opaque scalar/object data.
const instanceContentKey = "content"

// Translator resolves id-rooted changeset pointers against ONE decoded document.
// It is built once per document (NewTranslator) — it indexes every id-carrying
// instance to its physical RFC 6901 pointer up front — and then translates each
// operation's Path/From. It does not mutate the document.
type Translator struct {
	// index maps an instance id to the physical RFC 6901 pointer of that instance
	// node within the decoded document. Pre-order, last-wins on duplicate ids
	// (mirrors the resolver's buildIDIndex discipline; the schema documents ids as
	// unique, so duplicates are a best-effort tie-break, not a checked error here).
	index map[string]string
}

// NewTranslator indexes the decoded document tree, mapping every id-carrying
// instance to its physical RFC 6901 pointer so id-rooted pointers can be
// translated. doc is the document decoded into a generic JSON tree (map[string]any
// from encoding/json), as the apply path produces it from stored bytes. A nil or
// rootless document yields an empty index (only `$`-scope pointers will resolve);
// translation of an item-id pointer against it fails as not-found.
func NewTranslator(doc map[string]any) *Translator {
	t := &Translator{index: map[string]string{}}
	if doc == nil {
		return t
	}
	if root, ok := doc["root"].(map[string]any); ok {
		t.indexInstance(root, "/root")
	}
	return t
}

// indexInstance records inst's id (if any) at pointer, then descends into its
// physical instance-bearing locations: each `children[i]` slot and, for a block
// wrapper, its single `config/content` leaf. The walk is pre-order and shape-led —
// it descends wherever a nested instance can physically live, regardless of the
// node's item type, so it does not depend on resolving $refs.
func (t *Translator) indexInstance(inst map[string]any, pointer string) {
	if id, ok := inst["id"].(string); ok && id != "" {
		t.index[id] = pointer
	}

	// Block inner content: a single nested instance at config/content (a config
	// field, not a child slot). Descend it physically.
	if cfg, ok := inst["config"].(map[string]any); ok {
		if content, ok := cfg[instanceContentKey].(map[string]any); ok {
			t.indexInstance(content, pointer+"/config/"+instanceContentKey)
		}
	}

	// Child slots: a positional array of nested instances.
	if children, ok := inst["children"].([]any); ok {
		for i, child := range children {
			childInst, ok := child.(map[string]any)
			if !ok {
				continue
			}
			t.indexInstance(childInst, pointer+"/children/"+strconv.Itoa(i))
		}
	}
}

// Translate maps an id-rooted pointer to its physical RFC 6901 pointer within the
// indexed document. The pointer's leading segment is resolved (a `$`-scope to its
// document base, or an item id to its indexed physical pointer) and the literal
// RFC 6901 remainder is appended verbatim. opIndex is the operation index, carried
// into error Details for diagnostics. A malformed pointer (empty, not "/"-rooted,
// or an empty leading segment) fails with CHANGESET_POINTER_INVALID; an unknown
// item id fails with CHANGESET_TARGET_NOT_FOUND; an unknown `$`-scope reuses
// CONFIGURATOR_TARGET_SCOPE_UNKNOWN.
func (t *Translator) Translate(pointer string, opIndex int) (string, error) {
	if pointer == "" || pointer[0] != '/' {
		return "", errors.NewCodedErrorWithDetails(errors.CHANGESET_POINTER_INVALID,
			"changeset pointer is empty or not rooted at \"/\"",
			map[string]any{"pointer": pointer, "index": opIndex})
	}

	// Split the leading segment (the id or `$`-scope) from the literal RFC 6901
	// remainder. pointer[1:] drops the leading "/"; the first "/" after it (if any)
	// begins the remainder, which is appended to the resolved base verbatim.
	leading, remainder, _ := strings.Cut(pointer[1:], "/")
	if leading == "" {
		return "", errors.NewCodedErrorWithDetails(errors.CHANGESET_POINTER_INVALID,
			"changeset pointer has an empty leading id/scope segment",
			map[string]any{"pointer": pointer, "index": opIndex})
	}

	base, err := t.resolveLeading(leading, pointer, opIndex)
	if err != nil {
		return "", err
	}

	if remainder == "" {
		return base, nil
	}
	return base + "/" + remainder, nil
}

// resolveLeading maps a pointer's leading segment to its physical base pointer: a
// `$`-prefixed keyword routes to its reserved document scope (recognized BEFORE the
// id index is consulted, so it can never collide with an item id); otherwise the
// segment is an item id looked up in the physical id index. An unknown `$`-scope
// reuses CONFIGURATOR_TARGET_SCOPE_UNKNOWN; an unknown id is CHANGESET_TARGET_NOT_FOUND.
func (t *Translator) resolveLeading(leading, pointer string, opIndex int) (string, error) {
	if strings.HasPrefix(leading, reservedScopePrefix) {
		base, ok := reservedScopeBase[leading]
		if !ok {
			return "", errors.NewCodedErrorWithDetails(errors.CONFIGURATOR_TARGET_SCOPE_UNKNOWN,
				"changeset pointer names an unknown reserved document scope",
				map[string]any{"pointer": pointer, "scope": leading, "index": opIndex})
		}
		return base, nil
	}

	base, ok := t.index[leading]
	if !ok {
		return "", errors.NewCodedErrorWithDetails(errors.CHANGESET_TARGET_NOT_FOUND,
			"changeset pointer's leading id matches no item in the document",
			map[string]any{"pointer": pointer, "id": leading, "index": opIndex})
	}
	return base, nil
}

// TranslateOperation returns a copy of op with its Path (and From, for move/copy)
// translated from id-rooted to physical RFC 6901 pointers, ready for a standard
// RFC 6902 applier. opIndex is carried into any error's Details. The op's Value and
// the present/absent flags are preserved verbatim.
func (t *Translator) TranslateOperation(op Operation, opIndex int) (Operation, error) {
	path, err := t.Translate(op.Path, opIndex)
	if err != nil {
		return Operation{}, err
	}
	out := op
	out.Path = path

	if op.HasFrom {
		from, err := t.Translate(op.From, opIndex)
		if err != nil {
			return Operation{}, err
		}
		out.From = from
	}
	return out, nil
}

// TranslateChangeset translates every operation in cs against the document indexed
// by t, returning a new Changeset whose Path/From pointers are physical RFC 6901.
// It is fail-fast: the first untranslatable pointer stops the walk and is returned
// as a CodedError naming the offending operation index.
func (t *Translator) TranslateChangeset(cs *Changeset) (*Changeset, error) {
	out := &Changeset{Ops: make([]Operation, 0, len(cs.Ops))}
	for i, op := range cs.Ops {
		translated, err := t.TranslateOperation(op, i)
		if err != nil {
			return nil, err
		}
		out.Ops = append(out.Ops, translated)
	}
	return out, nil
}
