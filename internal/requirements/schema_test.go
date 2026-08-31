package requirements

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestPublishedSchemasAreDraft202012AndClosed(t *testing.T) {
	_, source, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "..", ".."))
	for _, name := range []string{"story", "story-revision", "story-event", "citation", "relation"} {
		path := filepath.Join(root, "schema", "v3", name+".schema.json")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		var schema map[string]any
		if err := json.Unmarshal(data, &schema); err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		if schema["$schema"] != "https://json-schema.org/draft/2020-12/schema" {
			t.Errorf("%s draft = %v", name, schema["$schema"])
		}
		if schema["additionalProperties"] != false {
			t.Errorf("%s root is not closed", name)
		}
	}

	data, err := os.ReadFile(filepath.Join(root, "schema", "v3", "story-revision.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var revision map[string]any
	if err := json.Unmarshal(data, &revision); err != nil {
		t.Fatal(err)
	}
	properties := revision["properties"].(map[string]any)
	criteria := properties["acceptance_criteria"].(map[string]any)
	items := criteria["items"].(map[string]any)
	if items["additionalProperties"] != false {
		t.Fatal("criterion object boundary is not closed")
	}
}
