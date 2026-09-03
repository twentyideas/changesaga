package saga

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func loadV4Schema(t *testing.T, name string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("../../schema/v4", name))
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		t.Fatal(err)
	}
	return value
}

func TestV4SchemasAreClosedAndVersioned(t *testing.T) {
	for _, name := range []string{"saga.schema.json", "deck.schema.json", "slide.schema.json", "item.schema.json"} {
		t.Run(name, func(t *testing.T) {
			schema := loadV4Schema(t, name)
			if schema["$schema"] != "https://json-schema.org/draft/2020-12/schema" || schema["additionalProperties"] != false {
				t.Fatalf("v4 schema is not closed draft 2020-12: %#v", schema)
			}
		})
	}
	if got := loadV4Schema(t, "saga.schema.json")["$id"]; got != V4SchemaURL {
		t.Fatalf("v4 saga schema id = %v", got)
	}
}

func TestV4ItemSchemaMakesCalloutAnEvidenceCapableItemKind(t *testing.T) {
	schema := loadV4Schema(t, "item.schema.json")
	kinds := dig(t, schema, "properties", "kind", "enum").([]any)
	found := false
	for _, kind := range kinds {
		found = found || kind == "callout"
	}
	if !found {
		t.Fatal("callout is not an Item kind")
	}
	if _, ok := schema["properties"].(map[string]any)["diffs"]; ok {
		t.Fatal("evidence belongs in the Item package's ___diffs records, not item.json")
	}
}
