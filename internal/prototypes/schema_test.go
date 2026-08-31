package prototypes

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestPublishedPrototypeSchemasAreDraft202012AndClosed(t *testing.T) {
	_, source, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "..", ".."))
	for _, name := range []string{"prototype", "prototype-revision", "prototype-annotation"} {
		data, err := os.ReadFile(filepath.Join(root, "schema", "v3", name+".schema.json"))
		if err != nil {
			t.Fatal(err)
		}
		var schema map[string]any
		if err := json.Unmarshal(data, &schema); err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		if schema["$schema"] != "https://json-schema.org/draft/2020-12/schema" || schema["$id"] != "https://changesaga.dev/schema/v3/"+name+".schema.json" {
			t.Errorf("%s publication metadata = %#v", name, schema)
		}
		if schema["additionalProperties"] != false {
			t.Errorf("%s root is not closed", name)
		}
	}
}

func TestNestedPrototypeSchemaObjectsAreClosed(t *testing.T) {
	_, source, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "..", ".."))
	load := func(name string) map[string]any {
		data, err := os.ReadFile(filepath.Join(root, "schema", "v3", name))
		if err != nil {
			t.Fatal(err)
		}
		var value map[string]any
		if err := json.Unmarshal(data, &value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	revision := load("prototype-revision.schema.json")
	sources := revision["properties"].(map[string]any)["source"].(map[string]any)["oneOf"].([]any)
	for i, source := range sources {
		if source.(map[string]any)["additionalProperties"] != false {
			t.Errorf("source branch %d is open", i)
		}
	}
	allowlist := sources[2].(map[string]any)["properties"].(map[string]any)["allowlist"].(map[string]any)
	if allowlist["additionalProperties"] != false {
		t.Error("provider allowlist object is open")
	}
	styles := revision["properties"].(map[string]any)["styles"].(map[string]any)["items"].(map[string]any)
	if styles["additionalProperties"] != false {
		t.Error("style object is open")
	}
	role := styles["properties"].(map[string]any)["roles"].(map[string]any)["items"].(map[string]any)
	if role["additionalProperties"] != false {
		t.Error("style role object is open")
	}
	annotation := load("prototype-annotation.schema.json")
	selectors := annotation["properties"].(map[string]any)["selector"].(map[string]any)["oneOf"].([]any)
	for i, selector := range selectors {
		if selector.(map[string]any)["additionalProperties"] != false {
			t.Errorf("selector branch %d is open", i)
		}
	}
	region := selectors[2].(map[string]any)["properties"].(map[string]any)["region"].(map[string]any)
	if region["additionalProperties"] != false {
		t.Error("normalized region object is open")
	}
}
