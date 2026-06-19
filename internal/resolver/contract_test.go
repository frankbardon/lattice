package resolver

import (
	"testing"

	"github.com/google/jsonschema-go/jsonschema"

	"github.com/frankbardon/lattice/errors"
	"github.com/frankbardon/lattice/internal/schema"
)

// TestResolveContractValid resolves the happy-path fixture (a table bound to a
// static connection whose inline rows conform to the table's expectedResult) and
// asserts the resolved tree carries the result-shape contract (item type,
// connection id, expected result).
func TestResolveContractValid(t *testing.T) {
	res := newRepoResolver(t)
	tree, err := res.Resolve("testdata/binding/valid-contract.json")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	if len(tree.Root.Children) != 1 {
		t.Fatalf("len(root.children) = %d, want 1", len(tree.Root.Children))
	}
	// Under the E3-S2 grammar the table is block-wrapped inside a body region:
	// root -> body region -> block -> table.
	table := tree.Root.Children[0].Children[0].Children[0]
	if table.Binding == nil {
		t.Fatalf("table.Binding is nil, want a resolved binding")
	}
	c := table.Binding.Contract
	if c == nil {
		t.Fatalf("table.Binding.Contract is nil, want a resolved contract")
	}
	if c.ItemType != "table" {
		t.Errorf("contract.ItemType = %q, want %q", c.ItemType, "table")
	}
	if c.ConnectionID != "inline-fruits" {
		t.Errorf("contract.ConnectionID = %q, want %q", c.ConnectionID, "inline-fruits")
	}
	if c.ExpectedResult == nil {
		t.Errorf("contract.ExpectedResult is nil, want the table's expectedResult fragment")
	}
	if got, _ := c.ExpectedResult["type"].(string); got != "array" {
		t.Errorf("contract.ExpectedResult[type] = %v, want \"array\"", c.ExpectedResult["type"])
	}
}

// TestResolveContractStaticViolating asserts a static connection whose inline
// data violates the consuming item's expectedResult fails fast with
// RESULT_SHAPE_INVALID, naming the offending instance path and connection id.
// The static connection's own config schema accepts the data (it permits null
// cells); the stricter table expectedResult is what rejects it — proving the
// contract pass runs after, and adds to, the connection pass.
func TestResolveContractStaticViolating(t *testing.T) {
	res := newRepoResolver(t)
	_, err := res.Resolve("testdata/binding/static-data-violating.json")
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !errors.HasCode(err, errors.RESULT_SHAPE_INVALID) {
		t.Fatalf("error = %v, want code %s", err, errors.RESULT_SHAPE_INVALID)
	}
	var ce *errors.CodedError
	if !asCoded(err, &ce) {
		t.Fatalf("error is not a CodedError: %v", err)
	}
	if got, _ := ce.Details["path"].(string); got != "root.children[0].children[0].children[0]" {
		t.Errorf("error path = %q, want %q", got, "root.children[0].children[0].children[0]")
	}
	if got, _ := ce.Details["connectionId"].(string); got != "inline-fruits" {
		t.Errorf("error connectionId = %q, want %q", got, "inline-fruits")
	}
}

// TestResolveContractUnit exercises resolveContract directly (table-driven) over
// hand-built graphs/instances/connections, covering the four contract outcomes
// independently of the full resolve pipeline: well-formed (with conforming and
// violating static data), malformed expectedResult, and a missing contract on a
// bound item.
func TestResolveContractUnit(t *testing.T) {
	// A well-formed expectedResult: an array of row objects whose cells are
	// non-null scalars.
	wellFormed := map[string]any{
		"type": "array",
		"items": map[string]any{
			"type": "object",
			"additionalProperties": map[string]any{
				"type": []any{"string", "number", "boolean"},
			},
		},
	}
	// A malformed expectedResult: "type" must be a string or array of strings,
	// not a number, so the fragment fails to compile.
	malformed := map[string]any{
		"type": float64(123),
	}

	conformingRows := []any{
		map[string]any{"name": "apple", "qty": float64(3)},
	}
	violatingRows := []any{
		map[string]any{"name": "apple", "qty": nil}, // null cell violates the scalar constraint
	}

	tests := []struct {
		name       string
		expected   map[string]any // nil => type declares no expectedResult
		connType   string         // connection-type name
		connConfig map[string]any
		wantCode   errors.Code // "" = success
	}{
		{
			name:       "well-formed contract, static data conforms",
			expected:   wellFormed,
			connType:   "static",
			connConfig: map[string]any{"rows": conformingRows},
		},
		{
			name:       "well-formed contract, static data violates",
			expected:   wellFormed,
			connType:   "static",
			connConfig: map[string]any{"rows": violatingRows},
			wantCode:   errors.RESULT_SHAPE_INVALID,
		},
		{
			name:       "malformed expectedResult fails fast",
			expected:   malformed,
			connType:   "static",
			connConfig: map[string]any{"rows": conformingRows},
			wantCode:   errors.CONTRACT_INVALID,
		},
		{
			name:     "bound item type declares no expectedResult",
			expected: nil,
			connType: "static",
			wantCode: errors.CONTRACT_MISSING,
		},
		{
			name:     "live connection is model-only: data not checked",
			expected: wellFormed,
			connType: "http",
			// Even non-conforming-looking config is ignored for non-static types.
			connConfig: map[string]any{"url": "https://x"},
		},
	}

	const typeID = "https://lattice.dev/schemas/items/table/1.0.0"
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := newContractGraph(t, typeID, "table", tc.expected)
			inst := &ResolvedInstance{Type: ResolvedTypeRef{ID: typeID, Name: "table"}}
			conn := &ResolvedConnection{
				ID:     "conn1",
				Type:   ResolvedTypeRef{Name: tc.connType},
				Config: tc.connConfig,
			}

			got, err := resolveContract(g, inst, conn, "root.children[0]")
			if tc.wantCode != "" {
				if err == nil {
					t.Fatalf("expected error, got nil (contract=%+v)", got)
				}
				if !errors.HasCode(err, tc.wantCode) {
					t.Fatalf("error = %v, want code %s", err, tc.wantCode)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveContract: %v", err)
			}
			if got == nil {
				t.Fatalf("contract is nil, want a resolved contract")
			}
			if got.ItemType != "table" {
				t.Errorf("contract.ItemType = %q, want %q", got.ItemType, "table")
			}
			if got.ConnectionID != "conn1" {
				t.Errorf("contract.ConnectionID = %q, want %q", got.ConnectionID, "conn1")
			}
		})
	}
}

// newContractGraph builds a minimal ResolvedGraph whose single item type carries
// the given expectedResult fragment in its schema Extra (or none when expected
// is nil), mirroring how google/jsonschema-go surfaces the schema-level keyword.
func newContractGraph(t *testing.T, typeID, name string, expected map[string]any) *schema.ResolvedGraph {
	t.Helper()
	sch := &jsonschema.Schema{ID: typeID, Type: "object"}
	if expected != nil {
		sch.Extra = map[string]any{expectedResultKey: expected}
	}
	return &schema.ResolvedGraph{
		Types: map[string]*schema.ResolvedType{
			typeID: {ID: typeID, Name: name, Schema: sch},
		},
	}
}
