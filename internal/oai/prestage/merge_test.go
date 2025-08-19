package prestage

import (
	"encoding/json"
	"testing"

	"github.com/hyperifyio/goagent/internal/oai"
)

func TestParsePrestagePayload_SupportsRoleSchemaAndKeyEntries(t *testing.T) {
	payload := `[
	  {"role":"system","content":"SYS"},
	  {"role":"developer","content":"D1"},
	  {"developer":"D2"},
	  {"tool_config": {"enable_tools":["http_fetch"], "hints": {"http_fetch.max_bytes": 1000}}},
	  {"image_instructions": {"style":"natural"}}
	]`
	parsed, err := ParsePrestagePayload(payload)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if parsed.System != "SYS" {
		t.Fatalf("system=%q", parsed.System)
	}
	if len(parsed.Developers) != 2 || parsed.Developers[0] != "D1" || parsed.Developers[1] != "D2" {
		t.Fatalf("developers=%v", parsed.Developers)
	}
	if parsed.ToolConfig == nil || len(parsed.ToolConfig.EnableTools) != 1 || parsed.ToolConfig.EnableTools[0] != "http_fetch" {
		t.Fatalf("tool_config=%v", parsed.ToolConfig)
	}
	if parsed.ImageInstructions == nil || parsed.ImageInstructions["style"].(string) != "natural" {
		t.Fatalf("image_instructions=%v", parsed.ImageInstructions)
	}
}

func TestMergePrestageIntoMessages_ReplacesSystemAndAppendsDevelopers(t *testing.T) {
	seed := []oai.Message{
		{Role: oai.RoleSystem, Content: "sys0"},
		{Role: oai.RoleDeveloper, Content: "cli-dev-1"},
		{Role: oai.RoleUser, Content: "user"},
	}
	parsed := PrestageParsed{System: "sys1", Developers: []string{"p-dev-1", "p-dev-2"}}
	merged := MergePrestageIntoMessages(seed, parsed)
	if merged[0].Content != "sys1" {
		t.Fatalf("system not replaced: %+v", merged)
	}
	// Expected order: system, cli-dev-1, p-dev-1, p-dev-2, user
	want := []string{oai.RoleSystem, oai.RoleDeveloper, oai.RoleDeveloper, oai.RoleDeveloper, oai.RoleUser}
	if len(merged) != len(want) {
		t.Fatalf("len=%d want %d", len(merged), len(want))
	}
	for i, r := range want {
		if merged[i].Role != r {
			t.Fatalf("role[%d]=%s want %s", i, merged[i].Role, r)
		}
	}
}

func TestParsePrestagePayload_IgnoresUnknownObjects(t *testing.T) {
	payload := `[{"foo":"bar"},{"developer":"D"}]`
	parsed, err := ParsePrestagePayload(payload)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(parsed.Developers) != 1 || parsed.Developers[0] != "D" {
		t.Fatalf("developers=%v", parsed.Developers)
	}
}

func TestParsePrestagePayload_SingleObject(t *testing.T) {
	obj := map[string]any{"system": "S"}
	b, _ := json.Marshal(obj)
	parsed, err := ParsePrestagePayload(string(b))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if parsed.System != "S" {
		t.Fatalf("system=%q", parsed.System)
	}
}
