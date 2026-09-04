package saga

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"testing"
)

// The published JSON Schemas are the normative contract, and this package is
// the reference implementation of it. Every rule below is checked twice: once
// against the schema text as shipped, and once against the Go predicate the
// runtime actually uses. A rule that only one side enforces is exactly the
// class of bug where a saga validates in one tool and is rejected by another.

const schemaDir = "../../schema/v2"

func loadSchema(t *testing.T, name string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(schemaDir, name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	var value map[string]any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	return value
}

func TestSchemaFilesAreWellFormed(t *testing.T) {
	entries, err := os.ReadDir(schemaDir)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		count++
		schema := loadSchema(t, entry.Name())
		for _, key := range []string{"$schema", "$id", "title"} {
			if _, ok := schema[key]; !ok {
				t.Errorf("%s: missing %s", entry.Name(), key)
			}
		}
		if schema["additionalProperties"] != false {
			t.Errorf("%s: v2 rejects unknown fields, so additionalProperties must be false", entry.Name())
		}
		if id, _ := schema["$id"].(string); id != "https://changesaga.dev/schema/v2/"+entry.Name() {
			t.Errorf("%s: $id %q does not match its path", entry.Name(), id)
		}
	}
	if count < 10 {
		t.Fatalf("expected the full v2 schema set, found %d files", count)
	}
}

// dig walks a decoded schema by key path so a moved or renamed keyword fails
// loudly instead of silently skipping the comparison.
func dig(t *testing.T, value any, path ...string) any {
	t.Helper()
	for i, key := range path {
		object, ok := value.(map[string]any)
		if !ok {
			t.Fatalf("schema path %v is not an object at %q", path[:i], key)
		}
		value, ok = object[key]
		if !ok {
			t.Fatalf("schema path %v has no %q", path[:i], key)
		}
	}
	return value
}

func schemaPattern(t *testing.T, value any, path ...string) *regexp.Regexp {
	t.Helper()
	text, ok := dig(t, value, path...).(string)
	if !ok {
		t.Fatalf("schema path %v is not a pattern string", path)
	}
	pattern, err := regexp.Compile(text)
	if err != nil {
		t.Fatalf("schema pattern %q does not compile: %v", text, err)
	}
	return pattern
}

func schemaEnum(t *testing.T, value any, path ...string) []string {
	t.Helper()
	raw, ok := dig(t, value, path...).([]any)
	if !ok {
		t.Fatalf("schema path %v is not an enum array", path)
	}
	result := make([]string, 0, len(raw))
	for _, item := range raw {
		text, ok := item.(string)
		if !ok {
			t.Fatalf("schema path %v contains a non-string enum value", path)
		}
		result = append(result, text)
	}
	sort.Strings(result)
	return result
}

// identityProbes exercise the boundaries of the stable identifier grammar from
// both directions: valid shapes that must survive, and every way an attacker or
// a careless author could try to smuggle a separator, a traversal segment, or
// an oversized value into a filename or URN.
var identityProbes = []string{
	"a", "A", "0", "saga", "saga-id", "saga_id", "saga.id", "a.b-c_d9",
	"20260819T120000.000000000Z-0a1b2c3d",
	"", " ", "  a", "a ", "-a", "_a", ".a", "..", ".", "../a", "a/b", "a\\b",
	"a:b", "a b", "a\tb", "a\nb", "a\x00b", "é", "a é", "🙂", "a?b", "a*b",
	"urn:change-saga:a:saga", "%2e%2e", "CON", "nul",
}

func init() {
	long := ""
	for len(long) < 200 {
		long += "abcdefghij"
	}
	identityProbes = append(identityProbes, long[:127], long[:128], long[:129], long[:200])
}

func TestStableIDGrammarMatchesEverySchema(t *testing.T) {
	files := map[string][]string{
		"saga.schema.json":         {"$defs", "id", "pattern"},
		"chapter.schema.json":      {"properties", "id", "pattern"},
		"section.schema.json":      {"properties", "id", "pattern"},
		"fragment.schema.json":     {"properties", "id", "pattern"},
		"message.schema.json":      {"properties", "id", "pattern"},
		"review.schema.json":       {"properties", "id", "pattern"},
		"thread.schema.json":       {"properties", "id", "pattern"},
		"thread-event.schema.json": {"properties", "id", "pattern"},
		"diff-review.schema.json":  {"properties", "id", "pattern"},
		"claim.schema.json":        {"properties", "id", "pattern"},
		"verification.schema.json": {"properties", "id", "pattern"},
	}
	for name, path := range files {
		schema := loadSchema(t, name)
		pattern := schemaPattern(t, schema, path...)
		for _, probe := range identityProbes {
			if got, want := pattern.MatchString(probe), ValidID(probe); got != want {
				t.Errorf("%s id %q: schema=%v runtime=%v", name, probe, got, want)
			}
		}
	}
}

func TestMarkdownAnchorGrammarMatchesLandmarkSchema(t *testing.T) {
	schema := loadSchema(t, "landmark.schema.json")
	selectors, ok := dig(t, schema, "properties", "selector", "oneOf").([]any)
	if !ok {
		t.Fatal("landmark selector is not a oneOf list")
	}
	patterns := []*regexp.Regexp{schemaPattern(t, schema, "properties", "id", "pattern")}
	for _, selector := range selectors {
		for _, field := range []string{"heading_id", "element_id"} {
			properties, _ := dig(t, selector, "properties").(map[string]any)
			if _, ok := properties[field]; ok {
				patterns = append(patterns, schemaPattern(t, selector, "properties", field, "pattern"))
			}
		}
	}
	if len(patterns) != 3 {
		t.Fatalf("expected the landmark id, heading_id, and element_id grammars, found %d", len(patterns))
	}
	probes := append([]string{"a", "a-b", "a1", "request-validation", "A", "1a", "-a", "a_b", "a.b", "a--b", "z"}, identityProbes...)
	long := ""
	for len(long) < 80 {
		long += "abcdefghij"
	}
	probes = append(probes, long[:63], long[:64], long[:65])
	for _, pattern := range patterns {
		for _, probe := range probes {
			if got, want := pattern.MatchString(probe), ValidMarkdownAnchor(probe); got != want {
				t.Errorf("anchor grammar %q for %q: schema=%v runtime=%v", pattern, probe, got, want)
			}
		}
	}
}

func TestMediaTypeGrammarMatchesFragmentSchema(t *testing.T) {
	pattern := schemaPattern(t, loadSchema(t, "fragment.schema.json"), "properties", "media_type", "pattern")
	probes := []string{
		"text/markdown", "text/html", "text/plain", "image/svg+xml", "image/png",
		"image/jpeg", "image/webp", "image/x-icon", "image/", "image", "image/a b",
		"image/../../etc/passwd", "image/png\r\nX-Injected: 1", "IMAGE/PNG",
		"text/markdown; charset=utf-8", "text/csv", "application/json", "",
		" text/html", "text/html ", "text/html\n",
	}
	for _, probe := range probes {
		if got, want := pattern.MatchString(probe), ValidMediaType(probe); got != want {
			t.Errorf("media_type %q: schema=%v runtime=%v", probe, got, want)
		}
	}
}

func TestEntrypointGrammarMatchesFragmentSchema(t *testing.T) {
	schema := loadSchema(t, "fragment.schema.json")
	entrypoint := dig(t, schema, "properties", "entrypoint")
	allow := schemaPattern(t, entrypoint, "pattern")
	denials, ok := dig(t, entrypoint, "not", "anyOf").([]any)
	if !ok {
		t.Fatal("entrypoint denials are not an anyOf list")
	}
	var deny []*regexp.Regexp
	for _, item := range denials {
		deny = append(deny, schemaPattern(t, item, "pattern"))
	}
	schemaAccepts := func(value string) bool {
		if value == "" || !allow.MatchString(value) {
			return false
		}
		for _, pattern := range deny {
			if pattern.MatchString(value) {
				return false
			}
		}
		return true
	}
	for _, probe := range entrypointProbes {
		if got, want := schemaAccepts(probe), EntrypointError(probe) == ""; got != want {
			t.Errorf("entrypoint %q: schema=%v runtime=%v (%s)", probe, got, want, EntrypointError(probe))
		}
	}
}

func TestAnnotationColorGrammarMatchesThreadSchema(t *testing.T) {
	schema := loadSchema(t, "thread.schema.json")
	patterns := map[string]*regexp.Regexp{
		"note":  schemaPattern(t, schema, "$defs", "note", "properties", "color", "pattern"),
		"text":  schemaPattern(t, schema, "$defs", "text", "properties", "color", "pattern"),
		"shape": schemaPattern(t, schema, "$defs", "shape", "properties", "color", "pattern"),
	}
	probes := []string{
		"#000000", "#ffffff", "#FFFFFF", "#f2bd4b", "#F2BD4B", "#abcdef",
		"#fff", "#1234567", "#12345", "ffffff", "red", "", "#gggggg",
		"#12345g", "rgb(1,2,3)", "expression(alert(1))", "url(javascript:1)",
		"#000000;background:url(x)", " #000000", "#000000 ", "#00000\n",
	}
	for name, pattern := range patterns {
		for _, probe := range probes {
			if got, want := pattern.MatchString(probe), ValidAnnotationColor(probe); got != want {
				t.Errorf("%s color %q: schema=%v runtime=%v", name, probe, got, want)
			}
		}
	}
}

func TestSchemaEnumsMatchRuntimeStates(t *testing.T) {
	reviewerKinds := schemaEnum(t, loadSchema(t, "review.schema.json"), "properties", "reviewer", "properties", "kind", "enum")
	if want := []string{"ai", "human"}; !equal(reviewerKinds, want) {
		t.Errorf("reviewer kinds %v, want %v", reviewerKinds, want)
	}
	approvals := schemaEnum(t, loadSchema(t, "review.schema.json"), "properties", "state", "enum")
	for _, state := range approvals {
		if !validReviewState(state) {
			t.Errorf("approval state %q is published but rejected by the runtime", state)
		}
	}
	for _, state := range []string{"approved", "rejected", "closed", "open"} {
		if !contains(approvals, state) {
			t.Errorf("approval state %q is accepted by the runtime but not published", state)
		}
	}
	if len(approvals) != 4 {
		t.Errorf("unexpected approval state set %v", approvals)
	}

	events := schemaEnum(t, loadSchema(t, "thread-event.schema.json"), "properties", "state", "enum")
	if want := []string{"open", "resolved", "withdrawn"}; !equal(events, want) {
		t.Errorf("thread event states %v, want %v", events, want)
	}
	diffReviews := schemaEnum(t, loadSchema(t, "diff-review.schema.json"), "properties", "state", "enum")
	if want := []string{"reviewed", "unreviewed"}; !equal(diffReviews, want) {
		t.Errorf("diff review states %v, want %v", diffReviews, want)
	}
	kinds := schemaEnum(t, loadSchema(t, "thread.schema.json"), "properties", "kind", "enum")
	if want := []string{"comment", "suggestion"}; !equal(kinds, want) {
		t.Errorf("thread kinds %v, want %v", kinds, want)
	}
	shapes := schemaEnum(t, loadSchema(t, "thread.schema.json"), "$defs", "shape", "properties", "type", "enum")
	if want := []string{"ellipse", "line", "path", "rect"}; !equal(shapes, want) {
		t.Errorf("shape types %v, want %v", shapes, want)
	}
	for _, shape := range shapes {
		anchor := Anchor{Type: "region", Coordinate: "normalized", Shapes: []Shape{{Type: shape}}}
		if err := ValidateAnchor(anchor); err != nil {
			t.Errorf("published shape %q is rejected by the runtime: %v", shape, err)
		}
	}
}

// TestAnchorTypesMatchThreadSchema keeps the anchor vocabulary in one place:
// every anchor branch the schema publishes must be a type the runtime knows,
// and the runtime must not accept a type the schema never described.
func TestAnchorTypesMatchThreadSchema(t *testing.T) {
	branches, ok := dig(t, loadSchema(t, "thread.schema.json"), "$defs", "anchor", "oneOf").([]any)
	if !ok {
		t.Fatal("anchor is not a oneOf list")
	}
	published := map[string]bool{}
	for _, branch := range branches {
		typeSchema, ok := dig(t, branch, "properties", "type").(map[string]any)
		if !ok {
			t.Fatal("anchor branch type is not an object")
		}
		if constant, ok := typeSchema["const"].(string); ok {
			published[constant] = true
			continue
		}
		for _, value := range schemaEnum(t, typeSchema, "enum") {
			published[value] = true
		}
	}
	want := map[string]bool{"target": true, "region": true, "drawing": true, "text": true, "note": true, "diff": true}
	if len(published) != len(want) {
		t.Fatalf("published anchor types %v, want %v", published, want)
	}
	for kind := range want {
		if !published[kind] {
			t.Errorf("anchor type %q is implemented but not published", kind)
		}
	}
	for kind := range published {
		if !want[kind] {
			t.Errorf("anchor type %q is published but not implemented", kind)
		}
		// An unknown type must be rejected outright; a known one must fail for
		// a content reason rather than the "unknown type" default branch.
		err := ValidateAnchor(Anchor{Type: kind})
		if err != nil && err.Error() == "anchor type must be target, region, drawing, text, note, or diff" {
			t.Errorf("published anchor type %q falls into the unknown-type branch", kind)
		}
	}
	if err := ValidateAnchor(Anchor{Type: "sticky"}); err == nil {
		t.Error("an unpublished anchor type must be rejected")
	}
}

func TestDiffURISchemaPatternsMatchTheirKinds(t *testing.T) {
	coverage := schemaPattern(t, loadSchema(t, "diff.schema.json"), "properties", "diffs", "items", "properties", "uri", "pattern")
	fileReview := schemaPattern(t, loadSchema(t, "diff-review.schema.json"), "properties", "uri", "pattern")
	cases := map[string][2]bool{
		"saga-diff://v1/line?a=b":  {true, false},
		"saga-diff://v1/event?a=b": {true, false},
		"saga-diff://v1/file?a=b":  {false, true},
		"saga-diff://v1/line":      {false, false},
		"saga-diff://v2/line?a=b":  {false, false},
		"http://v1/line?a=b":       {false, false},
		"":                         {false, false},
	}
	for uri, want := range cases {
		if got := coverage.MatchString(uri); got != want[0] {
			t.Errorf("coverage pattern on %q = %v, want %v", uri, got, want[0])
		}
		if got := fileReview.MatchString(uri); got != want[1] {
			t.Errorf("file review pattern on %q = %v, want %v", uri, got, want[1])
		}
	}
}

func TestNoteLengthLimitMatchesSchema(t *testing.T) {
	limit := dig(t, loadSchema(t, "thread.schema.json"), "$defs", "note", "properties", "text", "maxLength")
	number, ok := limit.(json.Number)
	if !ok {
		t.Fatalf("note maxLength is %T, not a number", limit)
	}
	value, err := number.Int64()
	if err != nil {
		t.Fatal(err)
	}
	if value != MaxNoteRunes {
		t.Fatalf("schema note maxLength %d does not match runtime MaxNoteRunes %d", value, MaxNoteRunes)
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func equal(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
