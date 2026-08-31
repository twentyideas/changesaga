package saga

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
)

const v3SchemaDir = "../../schema/v3"

func loadV3Schema(t *testing.T, name string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(v3SchemaDir, name))
	if err != nil {
		t.Fatalf("read v3 %s: %v", name, err)
	}
	var value map[string]any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		t.Fatalf("parse v3 %s: %v", name, err)
	}
	return value
}

func TestV3SagaManifestSchemaIsPublishedAtItsVersionedID(t *testing.T) {
	schema := loadV3Schema(t, "saga.schema.json")
	if got := schema["$schema"]; got != "https://json-schema.org/draft/2020-12/schema" {
		t.Errorf("v3 schema dialect = %q, want draft 2020-12", got)
	}
	if got := schema["$id"]; got != V3SchemaURL {
		t.Errorf("v3 schema $id = %q", got)
	}
	if got := schema["title"]; got != "Change Saga v3 manifest" {
		t.Errorf("v3 schema title = %q", got)
	}
	if schema["additionalProperties"] != false {
		t.Error("v3 manifest schema must remain closed")
	}
	if got := dig(t, schema, "properties", "version", "const"); got != json.Number(strconv.Itoa(CurrentSagaVersion)) {
		t.Errorf("v3 manifest version = %v, want %d", got, CurrentSagaVersion)
	}
}

func TestV3SagaManifestSchemaChangesOnlyContainerVersion(t *testing.T) {
	v2 := loadSchema(t, "saga.schema.json")
	v3 := loadV3Schema(t, "saga.schema.json")

	delete(v2, "$id")
	delete(v2, "title")
	delete(v3, "$id")
	delete(v3, "title")
	dig(t, v3, "properties", "version").(map[string]any)["const"] = json.Number("2")

	if !reflect.DeepEqual(v3, v2) {
		t.Errorf("v3 saga manifest must retain the complete v2 field contract apart from its version, $id, and title\nv2: %#v\nv3: %#v", v2, v3)
	}
}

func TestV3SagaManifestIDGrammarMatchesRuntime(t *testing.T) {
	pattern := schemaPattern(t, loadV3Schema(t, "saga.schema.json"), "$defs", "id", "pattern")
	for _, probe := range identityProbes {
		if got, want := pattern.MatchString(probe), ValidID(probe); got != want {
			t.Errorf("v3 saga id %q: schema=%v runtime=%v", probe, got, want)
		}
	}
}
