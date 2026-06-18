package layoutagent

import "testing"

// TestParsePlan_Valid accepts a well-formed plan with each action type and
// decodes the fields the coordinator consumes.
func TestParsePlan_Valid(t *testing.T) {
	reply := `{
      "actions": [
        {"type":"create_brick","position":{"x":0,"y":0},"size":{"width":6,"height":4},"prompt":"bar chart of sales by region"},
        {"type":"move_brick","brick_id":"b1","position":{"x":6,"y":0}},
        {"type":"resize_brick","brick_id":"b2","size":{"width":12,"height":3}},
        {"type":"delete_brick","brick_id":"b3"}
      ]
    }`
	plan, err := parsePlan(reply)
	if err != nil {
		t.Fatalf("parsePlan: %v", err)
	}
	if len(plan.Actions) != 4 {
		t.Fatalf("actions = %d, want 4", len(plan.Actions))
	}
	c := plan.Actions[0]
	if c.Type != ActionCreateBrick || c.Position == nil || c.Size == nil || c.Prompt == "" {
		t.Fatalf("create_brick not decoded: %+v", c)
	}
	if c.Position.X != 0 || c.Size.Width != 6 {
		t.Fatalf("create_brick fields wrong: %+v", c)
	}
}

// TestParsePlan_FencedAndProse confirms the loop extracts the object even when
// the model wraps it in a ```json fence or stray prose.
func TestParsePlan_FencedAndProse(t *testing.T) {
	body := `{"actions":[{"type":"delete_brick","brick_id":"b1"}]}`
	cases := map[string]string{
		"fenced":    "```json\n" + body + "\n```",
		"proseWrap": "Here is the plan:\n" + body + "\nDone.",
	}
	for name, reply := range cases {
		t.Run(name, func(t *testing.T) {
			plan, err := parsePlan(reply)
			if err != nil {
				t.Fatalf("parsePlan: %v", err)
			}
			if len(plan.Actions) != 1 {
				t.Fatalf("actions = %d, want 1", len(plan.Actions))
			}
		})
	}
}

// TestParsePlan_Rejects rejects malformed plans (bad JSON, unknown action,
// missing required fields, extra keys) with InvalidOutput.
func TestParsePlan_Rejects(t *testing.T) {
	cases := map[string]string{
		"notJSON":          "I cannot do that.",
		"noActions":        `{"foo": 1}`,
		"unknownType":      `{"actions":[{"type":"frobnicate"}]}`,
		"createNoPosition": `{"actions":[{"type":"create_brick","size":{"width":6,"height":4},"prompt":"x"}]}`,
		"createNoPrompt":   `{"actions":[{"type":"create_brick","position":{"x":0,"y":0},"size":{"width":6,"height":4}}]}`,
		"moveNoBrick":      `{"actions":[{"type":"move_brick","position":{"x":0,"y":0}}]}`,
		"resizeNoSize":     `{"actions":[{"type":"resize_brick","brick_id":"b1"}]}`,
		"deleteNoBrick":    `{"actions":[{"type":"delete_brick"}]}`,
		"extraKey":         `{"actions":[{"type":"delete_brick","brick_id":"b1","evil":1}]}`,
		"zeroWidth":        `{"actions":[{"type":"create_brick","position":{"x":0,"y":0},"size":{"width":0,"height":4},"prompt":"x"}]}`,
	}
	for name, reply := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := parsePlan(reply); CodeOf(err) != InvalidOutput {
				t.Fatalf("code = %s, want %s (%v)", CodeOf(err), InvalidOutput, err)
			}
		})
	}
}

// TestPlanSchemaCompiles is implicitly covered by package init (mustCompile
// panics on a bad schema), but assert it explicitly for clarity.
func TestPlanSchemaCompiles(t *testing.T) {
	if compiledPlanSchema == nil {
		t.Fatal("PlanSchema failed to compile")
	}
}
