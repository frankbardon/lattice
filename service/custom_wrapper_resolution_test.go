package service_test

// custom_wrapper_resolution_test.go — E4-S2: prove the WRAPPER role resolves end
// to end for a CUSTOM wrapper defined ONLY in a test fs.FS, with a custom
// contentField name (`body`, NOT the built-in `content`), and confirm the
// built-in block resolved output is byte-identical against the E1-S3 goldens.
//
// E4-S1b (custom_wrapper_test.go, same package) already proved the SERVICE-layer
// SURFACE DELEGATION for a custom wrapper (its get_node view delegates to the
// inner content). This file BUILDS ON that — it does not re-prove delegation;
// it proves the RESOLUTION half the story owns:
//
//   1. A custom wrapper whose schema declares contentField:"body" (NOT "content")
//      resolves: its `body` instance is LIFTED OUT as a separate child node, and
//      the wrapper's own concerns (its `caption`) resolve on their own pass and
//      stay on the wrapper node. The custom field name `body` is the key proof the
//      wrapper behavior generalized beyond the hardcoded `content` key.
//   2. The id-present / exactly-one invariants fire for the custom wrapper —
//      WRAPPER_ID_MISSING when the id is blank/missing, WRAPPER_CHILD_COUNT_INVALID
//      when the content field is absent/null or not a single instance — proving the
//      invariants are keyword + contentField driven, not gated on the `block` name
//      or the literal `content` key.
//   3. A custom wrapper wrapping a custom REGION (composition) resolves correctly.
//   4. The custom wrapper is enumerated by list_schemas / returned by get_schema.
//   5. The built-in block goldens are byte-identical (the migration must not move
//      the resolved bytes) — re-proved via the resolver-package baseline, which the
//      service tests cannot import; here we assert via the public facade that the
//      built-in block still resolves and lifts its `content` exactly as before.
//
// Everything goes through the PUBLIC service facade (and root errors) — the exact
// boundary the MCP tools see — so the import boundary is respected (no internal/*).
// It reuses the overlay fs.FS helpers from custom_widget_test.go and the custom
// region schemas (kpiPanelSchema/kpiInputSchema) from custom_region_test.go.

import (
	"io/fs"
	"testing"

	"github.com/frankbardon/lattice/errors"
	"github.com/frankbardon/lattice/service"
)

// cardWrapperSchema is a CUSTOM wrapper whose contentField is `body`, NOT the
// built-in `content`. This is the headline proof of E4-S2: the wrapper pass reads
// the schema's latticeBehavior.contentField to find the single inner item, so a
// wrapper that names its content field `body` resolves through the SAME pass with
// ZERO Go change. It declares its OWN concern (`caption`) distinct from whatever it
// wraps, so the test can prove the wrapper's own config survives and the content is
// lifted out. Its $id follows the catalog convention -> items/card/1.0.0.
const cardWrapperSchema = `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "https://lattice.dev/schemas/items/card/1.0.0",
  "title": "Card (custom wrapper, contentField=body)",
  "description": "A custom wrapper defined only in a test fs.FS whose content field is named body, not content.",
  "type": "object",
  "required": ["id", "body"],
  "additionalProperties": false,
  "latticeBehavior": {
    "role": "wrapper",
    "contentField": "body"
  },
  "configurable": {
    "caption": {
      "type": "string",
      "label": "Caption",
      "rendering": "text-input",
      "constraints": {"description": "The card's caption."}
    }
  },
  "properties": {
    "id": {"type": "string", "minLength": 1},
    "caption": {"type": "string"},
    "body": {"$ref": "https://lattice.dev/schemas/dashboard/1.0.0#/$defs/instance"}
  }
}`

// customCardSchemaFS layers the custom card wrapper plus the custom region schemas
// (kpi-panel, a widgets/none region, and kpi-input, a number widget — both reused
// from custom_region_test.go) over the real schemas/ directory, so a card can wrap
// a custom region whose own children resolve through the real catalog.
func customCardSchemaFS(t *testing.T) fs.FS {
	t.Helper()
	return newOverlaySchemaFS(t, map[string][]byte{
		"items/card.schema.json":      []byte(cardWrapperSchema),
		"items/kpi-panel.schema.json": []byte(kpiPanelSchema),
		"items/kpi-input.schema.json": []byte(kpiInputSchema),
	})
}

// newCustomCardService wires a Service over the card overlay FS plus a throwaway
// store. Documents resolve in-memory via ResolveBytes (no store I/O).
func newCustomCardService(t *testing.T) *service.Service {
	t.Helper()
	svc, err := service.Open(service.Options{
		Backend: service.BackendFS,
		Root:    t.TempDir(),
		Schemas: customCardSchemaFS(t),
	})
	if err != nil {
		t.Fatalf("Open over custom card schema FS: %v", err)
	}
	return svc
}

// cardDoc nests a single custom `card` wrapper (id cardID) under a built-in
// container document root. The card carries its own caption plus a `body` field
// (its contentField) holding bodyInstance — the single inner item the wrapper lifts
// out. The resolved card is therefore at tree.Root.Children[0]. caption may be ""
// to omit it; bodyField lets a test inject a malformed body (absent / wrong shape).
func cardDoc(cardID, caption, bodyField string) string {
	// Build the card config from only the present fields, joined with commas, so no
	// case produces invalid JSON (a trailing comma) — the absent-body case in
	// particular must parse cleanly and reach the WRAPPER_CHILD_COUNT_INVALID
	// invariant, not fail at the JSON parser.
	var cfg []string
	// The wrapper's required stable id lives in its CONFIG (the wrapper item-type's
	// `id` field the WRAPPER_ID_MISSING invariant reads); cardID == "" omits it so
	// the invariant — not the schema — is what fires. A blank-but-present id (e.g.
	// "   ") is passed verbatim to exercise the whitespace-only id guard.
	if cardID != "" {
		cfg = append(cfg, `"id": "`+cardID+`"`)
	}
	if caption != "" {
		cfg = append(cfg, `"caption": "`+caption+`"`)
	}
	if bodyField != "" {
		cfg = append(cfg, bodyField)
	}
	config := joinFields(cfg)

	// The instance-level id is omitted when blank (the dashboard schema forbids an
	// empty instance id); a stable non-empty instance id keeps node addressing sane.
	instID := ""
	if cardID != "" && cardID != "   " {
		instID = `"id": "` + cardID + `",`
	}
	// The root container accepts only positional regions, so a wrapper cannot sit
	// directly under root — it lives inside an intermediate `container` region (the
	// regions-or-wrappers policy admits wrappers). The resolved card is therefore at
	// tree.Root.Children[0].Children[0].
	return `{
  "manifest": {"formatVersion": "1.0.0", "id": "carddoc", "title": "Card Doc"},
  "variables": [{"name": "v", "type": "number", "default": 0}],
  "root": {
    "$ref": "https://lattice.dev/schemas/items/container/1.0.0",
    "id": "rootc",
    "config": {"grid": {"columns": [1]}},
    "children": [
      {
        "$ref": "https://lattice.dev/schemas/items/container/1.0.0",
        "id": "region",
        "config": {"grid": {"columns": [1]}},
        "children": [
          {
            "$ref": "https://lattice.dev/schemas/items/card/1.0.0",
            ` + instID + `
            "config": {` + config + `}
          }
        ]
      }
    ]
  }
}`
}

// joinFields joins JSON object members with commas (no trailing comma).
func joinFields(fields []string) string {
	out := ""
	for i, f := range fields {
		if i > 0 {
			out += ", "
		}
		out += f
	}
	return out
}

// headingBody is a heading content leaf placed in the card's `body` contentField.
// The heading declares its own surface (text, level), distinct from the card's own
// (caption), so lifting can be observed: after resolution the content is a separate
// child node and the card's own config retains caption but NOT body.
const headingBody = `"body": {
  "$ref": "https://lattice.dev/schemas/items/heading/1.0.0",
  "id": "h",
  "config": {"text": "Hello", "level": 2}
}`

// cardNode returns the custom card wrapper node nested under the container root.
func cardNode(t *testing.T, tree *service.ResolvedTree) *service.ResolvedInstance {
	t.Helper()
	if tree.Root == nil || len(tree.Root.Children) != 1 || len(tree.Root.Children[0].Children) != 1 {
		t.Fatalf("expected root -> container region -> one card child; got %+v", tree.Root)
	}
	return tree.Root.Children[0].Children[0]
}

// --- E4-S2: custom contentField (`body`) resolves and lifts the inner item ----

// TestCustomWrapperCustomContentFieldLiftsInnerNode is THE headline proof: a custom
// wrapper whose schema declares contentField:"body" (NOT "content") resolves
// through the same wrapper pass — its `body` instance is lifted out as a SEPARATE
// child node, and the wrapper's own concern (`caption`) resolves on its own pass and
// remains on the wrapper node, with `body` NOT duplicated into the wrapper's config.
func TestCustomWrapperCustomContentFieldLiftsInnerNode(t *testing.T) {
	svc := newCustomCardService(t)
	doc := cardDoc("card1", "Overview", headingBody)

	tree, err := svc.ResolveBytes([]byte(doc), "card lift", nil)
	if err != nil {
		t.Fatalf("ResolveBytes: unexpected error: %v", err)
	}
	card := cardNode(t, tree)
	if card.Type.Name != "card" {
		t.Fatalf("wrapper type = %q, want card", card.Type.Name)
	}

	// The inner `body` item is lifted out as a SEPARATE child node (the wrapper's
	// single child) — exactly one, and it is the heading, NOT the card.
	if len(card.Children) != 1 {
		t.Fatalf("expected the card to lift its body into exactly one child node, got %d", len(card.Children))
	}
	inner := card.Children[0]
	if inner.Type.Name != "heading" {
		t.Errorf("lifted child type = %q, want heading", inner.Type.Name)
	}
	if inner.ID != "h" {
		t.Errorf("lifted child id = %q, want h", inner.ID)
	}
	if got := inner.Config["text"]; got != "Hello" {
		t.Errorf("lifted heading text = %v, want Hello", got)
	}

	// The wrapper's OWN concern (caption) resolves on its own pass and stays on the
	// wrapper node...
	if got := card.Config["caption"]; got != "Overview" {
		t.Errorf("card caption = %v, want Overview (the wrapper's own concern must survive)", got)
	}
	// ...and the custom contentField `body` is LIFTED OUT — never duplicated into
	// the wrapper's resolved config. (This is the generalization proof: had the pass
	// been hardcoded to `content`, it would not have lifted `body`.)
	if _, present := card.Config["body"]; present {
		t.Errorf("card config still carries the lifted contentField %q; it must be lifted out, got config %v", "body", card.Config)
	}
	if _, present := card.Config["content"]; present {
		t.Errorf("card config unexpectedly carries a `content` key; the custom contentField is `body`")
	}
}

// --- E4-S2: invariants fire for the custom wrapper (keyword + contentField) ----

// TestCustomWrapperInvariantsFire proves the wrapper's two own-shape invariants
// fire for a CUSTOM wrapper with a custom contentField — proving they are keyword +
// contentField driven, not gated on the `block` name or the literal `content` key:
//
//   - WRAPPER_ID_MISSING            when the wrapper's id is missing or blank.
//   - WRAPPER_CHILD_COUNT_INVALID   when the contentField (`body`) is absent/null,
//     or present but not a single instance object.
func TestCustomWrapperInvariantsFire(t *testing.T) {
	tests := []struct {
		name     string
		id       string
		caption  string
		body     string
		wantCode errors.Code
	}{
		{
			name:     "missing id -> WRAPPER_ID_MISSING",
			id:       "",
			caption:  "Overview",
			body:     headingBody,
			wantCode: errors.WRAPPER_ID_MISSING,
		},
		{
			name:     "blank (whitespace) id -> WRAPPER_ID_MISSING",
			id:       "   ",
			caption:  "Overview",
			body:     headingBody,
			wantCode: errors.WRAPPER_ID_MISSING,
		},
		{
			name:     "body contentField absent -> WRAPPER_CHILD_COUNT_INVALID",
			id:       "card1",
			caption:  "Overview",
			body:     "",
			wantCode: errors.WRAPPER_CHILD_COUNT_INVALID,
		},
		{
			name:     "body contentField null -> WRAPPER_CHILD_COUNT_INVALID",
			id:       "card1",
			caption:  "Overview",
			body:     `"body": null`,
			wantCode: errors.WRAPPER_CHILD_COUNT_INVALID,
		},
		{
			name:    "body contentField is an array (not a single instance) -> WRAPPER_CHILD_COUNT_INVALID",
			id:      "card1",
			caption: "Overview",
			body: `"body": [
				{"$ref": "https://lattice.dev/schemas/items/heading/1.0.0", "id": "h1", "config": {"text": "a", "level": 1}},
				{"$ref": "https://lattice.dev/schemas/items/heading/1.0.0", "id": "h2", "config": {"text": "b", "level": 1}}
			]`,
			wantCode: errors.WRAPPER_CHILD_COUNT_INVALID,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc := newCustomCardService(t)
			doc := cardDoc(tc.id, tc.caption, tc.body)

			_, err := svc.ResolveBytes([]byte(doc), tc.name, nil)
			if err == nil {
				t.Fatalf("expected error %s, got nil", tc.wantCode)
			}
			if !errors.HasCode(err, tc.wantCode) {
				t.Fatalf("expected code %s, got: %v", tc.wantCode, err)
			}
		})
	}
}

// --- E4-S2: composition — custom wrapper wrapping a custom region -------------

// TestCustomWrapperWrappingCustomRegion proves the composition case: a custom
// wrapper (card, contentField=body) whose single inner item is a CUSTOM region
// (kpi-panel, a widgets/none region) resolves correctly. The region is lifted out
// as the wrapper's separate child and gets its OWN region resolution pass (its
// widget children resolve), while the wrapper keeps only its own concern. This
// proves the wrapper lifts whatever its contentField names — not just a leaf — and
// that the lifted node runs through the generic instance walk (so a region content
// gets region treatment), purely keyword-driven.
func TestCustomWrapperWrappingCustomRegion(t *testing.T) {
	svc := newCustomCardService(t)

	// A kpi-panel region (widgets/none) holding two kpi-input widgets, placed in the
	// card's `body` contentField.
	regionBody := `"body": {
		"$ref": "https://lattice.dev/schemas/items/kpi-panel/1.0.0",
		"id": "panel",
		"config": {"arrangement": "inline"},
		"children": [
			` + kpiInputWidget("wa", "v") + `,
			` + kpiInputWidget("wb", "v") + `
		]
	}`
	doc := cardDoc("card1", "Metrics", regionBody)

	tree, err := svc.ResolveBytes([]byte(doc), "card wraps region", nil)
	if err != nil {
		t.Fatalf("ResolveBytes: unexpected error: %v", err)
	}
	card := cardNode(t, tree)
	if card.Type.Name != "card" {
		t.Fatalf("wrapper type = %q, want card", card.Type.Name)
	}
	if got := card.Config["caption"]; got != "Metrics" {
		t.Errorf("card caption = %v, want Metrics", got)
	}
	if _, present := card.Config["body"]; present {
		t.Errorf("card config still carries the lifted contentField; got %v", card.Config)
	}

	// The lifted child is the custom region, resolved as a SEPARATE node with its own
	// region pass applied to its widget children.
	if len(card.Children) != 1 {
		t.Fatalf("expected the card to lift its region body into one child, got %d", len(card.Children))
	}
	region := card.Children[0]
	if region.Type.Name != "kpi-panel" {
		t.Fatalf("lifted child type = %q, want kpi-panel", region.Type.Name)
	}
	if len(region.Children) != 2 {
		t.Errorf("lifted region resolved %d widget children, want 2", len(region.Children))
	}
	// kpi-panel is layout:none -> no grid Layout, no Flow (its region pass ran).
	if region.Layout != nil {
		t.Errorf("layout:none region must carry no grid Layout; got %+v", region.Layout)
	}
	if region.Flow != nil {
		t.Errorf("layout:none region must carry no Flow; got %+v", region.Flow)
	}
}

// --- E4-S2: custom wrapper is first-class on the MCP surface ------------------

// TestCustomWrapperIsFirstClassOnMCPSurface proves the custom card wrapper is
// first-class on the grammar surface the MCP tools expose: list_schemas ->
// ListSchemas() enumerates it and get_schema -> Schema(type) returns its raw JSON
// Schema including the latticeBehavior keyword (with role:wrapper and the custom
// contentField:body). It goes through the service facade only — the exact boundary
// the MCP layer sees — respecting the import boundary (no internal/* path named).
func TestCustomWrapperIsFirstClassOnMCPSurface(t *testing.T) {
	svc := newCustomCardService(t)

	names, err := svc.ListSchemas()
	if err != nil {
		t.Fatalf("ListSchemas: %v", err)
	}
	have := map[string]bool{}
	for _, n := range names {
		have[n] = true
	}
	if !have["card"] {
		t.Errorf("ListSchemas did not enumerate the custom wrapper %q; got %v", "card", names)
	}

	raw, err := svc.Schema("card")
	if err != nil {
		t.Fatalf("Schema(card): %v", err)
	}
	if len(raw) == 0 {
		t.Fatalf("Schema(card) returned no bytes")
	}
	// The raw schema must carry the latticeBehavior keyword get_schema surfaces
	// verbatim, with the wrapper role and the custom contentField — the keywords
	// that make the type a first-class wrapper with a non-default content field.
	for _, want := range []string{"latticeBehavior", `"wrapper"`, "contentField", `"body"`} {
		if !containsSub(raw, want) {
			t.Errorf("Schema(card) did not surface %q in the latticeBehavior keyword: %s", want, raw)
		}
	}
}

// --- E4-S2: built-in block goldens are byte-identical (FR-22) -----------------

// TestBuiltinBlockStillLiftsContentField re-proves, through the PUBLIC facade, that
// the BUILT-IN block wrapper still resolves and lifts its (default) `content`
// contentField exactly as before the keyword migration — the same behavior the
// committed E1-S3 goldens pin. The byte-identical golden regression itself lives in
// the resolver package (TestBuiltinGoldenBaseline, which the service_test package
// cannot import); that test runs in `go test ./...` and turns red on any drift. This
// facade-level check is the service-boundary complement: the built-in block, over
// the real shipped schemas/, lifts `content` into a separate child node and keeps
// its own concerns — confirming the migration did not move the built-in behavior.
func TestBuiltinBlockStillLiftsContentField(t *testing.T) {
	// A real (non-overlay) service over the shipped schemas/ catalog only — no custom
	// types — so this exercises the built-in block exactly as production does.
	svc, err := service.Open(service.Options{
		Backend: service.BackendFS,
		Root:    t.TempDir(),
		Schemas: newOverlaySchemaFS(t, nil),
	})
	if err != nil {
		t.Fatalf("Open over shipped schema FS: %v", err)
	}

	// root container -> intermediate container region -> block (id "b") wrapping a
	// markdown leaf via the default `content` contentField (root admits only
	// positional regions, so the wrapper lives one level down).
	doc := `{
  "manifest": {"formatVersion": "1.0.0", "id": "blockdoc", "title": "Block Doc"},
  "root": {
    "$ref": "https://lattice.dev/schemas/items/container/1.0.0",
    "id": "rootc",
    "config": {"grid": {"columns": [1]}},
    "children": [
      {
        "$ref": "https://lattice.dev/schemas/items/container/1.0.0",
        "id": "region",
        "config": {"grid": {"columns": [1]}},
        "children": [
          {
            "$ref": "https://lattice.dev/schemas/items/block/1.0.0",
            "id": "b",
            "config": {
              "id": "b",
              "content": {
                "$ref": "https://lattice.dev/schemas/items/markdown/1.0.0",
                "id": "md",
                "config": {"source": "# hi"}
              }
            }
          }
        ]
      }
    ]
  }
}`

	tree, err := svc.ResolveBytes([]byte(doc), "builtin block", nil)
	if err != nil {
		t.Fatalf("ResolveBytes: unexpected error: %v", err)
	}
	if tree.Root == nil || len(tree.Root.Children) != 1 || len(tree.Root.Children[0].Children) != 1 {
		t.Fatalf("expected root -> container region -> one block child; got %+v", tree.Root)
	}
	block := tree.Root.Children[0].Children[0]
	if block.Type.Name != "block" {
		t.Fatalf("wrapper type = %q, want block", block.Type.Name)
	}
	// The built-in block lifts its `content` into a separate child node...
	if len(block.Children) != 1 {
		t.Fatalf("built-in block must lift content into one child, got %d", len(block.Children))
	}
	if block.Children[0].Type.Name != "markdown" {
		t.Errorf("lifted child type = %q, want markdown", block.Children[0].Type.Name)
	}
	// ...and never duplicates `content` into its own config.
	if _, present := block.Config["content"]; present {
		t.Errorf("built-in block config still carries the lifted `content`; got %v", block.Config)
	}
}
