package connector

import (
	"bytes"
	"strings"
	"testing"

	"github.com/twentyideas/changesaga/internal/saga"
)

var testComparison = Comparison{
	Repository: "https://example.test/acme/app.git",
	Base:       "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	Head:       "product-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
}

func lineRecord(side string, start, end int, note string) Record {
	return Record{Comparison: testComparison, Note: note, Kind: "lines", Side: side, Start: start, End: end, Path: "internal/api.go"}
}

func encode(t *testing.T, file File) string {
	t.Helper()
	var buffer bytes.Buffer
	if err := Write(&buffer, file); err != nil {
		t.Fatalf("write: %v", err)
	}
	return buffer.String()
}

func decode(t *testing.T, text string) File {
	t.Helper()
	file, err := Parse(strings.NewReader(text))
	if err != nil {
		t.Fatalf("parse: %v\n%s", err, text)
	}
	return file
}

func TestWriteThenParseRecoversEveryRecord(t *testing.T) {
	file := File{
		Owner:  "urn:change-saga:demo:fragment:api",
		Source: "internal/api.go",
		Records: []Record{
			lineRecord("new", 18, 42, "The validated request crosses into persistence."),
			lineRecord("old", 7, 7, "The validated request crosses into persistence."),
			{Comparison: testComparison, Kind: "event", Event: "add", Path: "internal/api.go"},
			{Comparison: testComparison, Kind: "event", Event: "rename", OldPath: "internal/old api.go", NewPath: "internal/api.go", Note: "Moved"},
		},
	}
	encoded := encode(t, file)
	decoded := decode(t, encoded)

	if decoded.Owner != file.Owner || decoded.Source != file.Source {
		t.Fatalf("header round trip lost identity: %+v", decoded)
	}
	if len(decoded.Records) != len(file.Records) {
		t.Fatalf("records = %d, want %d\n%s", len(decoded.Records), len(file.Records), encoded)
	}
	expected := append([]Record(nil), file.Records...)
	SortRecords(expected)
	for i := range expected {
		if decoded.Records[i] != expected[i] {
			t.Fatalf("record %d round trip:\n got %+v\nwant %+v", i, decoded.Records[i], expected[i])
		}
	}
}

// A path with a space is legal in Git and shares its line with other fields.
// If it were not escaped, one path would decode as two and a rename would be
// silently rewritten.
func TestPathsWithSpacesSurviveTheRecordLine(t *testing.T) {
	file := File{
		Owner:  "urn:change-saga:demo:saga",
		Source: "docs/design notes.md",
		Records: []Record{
			{Comparison: testComparison, Kind: "event", Event: "add", Path: "docs/design notes.md"},
		},
	}
	decoded := decode(t, encode(t, file))
	if decoded.Source != "docs/design notes.md" {
		t.Fatalf("source = %q", decoded.Source)
	}
	if decoded.Records[0].Path != "docs/design notes.md" {
		t.Fatalf("path = %q", decoded.Records[0].Path)
	}
}

func TestMultiLineNotesSurviveRoundTrip(t *testing.T) {
	note := "First line.\nSecond line, with a trailing backslash \\\nThird."
	file := File{
		Owner: "urn:change-saga:demo:saga", Source: "internal/api.go",
		Records: []Record{lineRecord("new", 1, 3, note)},
	}
	decoded := decode(t, encode(t, file))
	if decoded.Records[0].Note != note {
		t.Fatalf("note = %q, want %q", decoded.Records[0].Note, note)
	}
}

func TestEncodingIsCanonicalRegardlessOfInputOrder(t *testing.T) {
	forward := File{Owner: "o", Source: "internal/api.go", Records: []Record{
		lineRecord("new", 1, 4, "a"), lineRecord("new", 9, 9, "b"), lineRecord("old", 2, 2, "a"),
	}}
	backward := File{Owner: "o", Source: "internal/api.go", Records: []Record{
		lineRecord("old", 2, 2, "a"), lineRecord("new", 9, 9, "b"), lineRecord("new", 1, 4, "a"),
	}}
	if encode(t, forward) != encode(t, backward) {
		t.Fatalf("two orderings of the same evidence produced different bytes:\n%s\n---\n%s",
			encode(t, forward), encode(t, backward))
	}
}

// Aliases are content addressed so that adding evidence never rewrites the
// lines already in the file. A positional alias would renumber every record the
// moment a note sorted ahead of an existing one, which turns every concurrent
// addition into a whole-file conflict.
func TestAddingANoteDoesNotRewriteExistingRecordLines(t *testing.T) {
	before := File{Owner: "o", Source: "internal/api.go", Records: []Record{
		lineRecord("new", 100, 120, "zebra"),
	}}
	after := File{Owner: "o", Source: "internal/api.go", Records: []Record{
		lineRecord("new", 100, 120, "zebra"),
		lineRecord("new", 200, 210, "aardvark"),
	}}
	existing := recordLines(encode(t, before))
	if len(existing) != 1 {
		t.Fatalf("expected one record line, got %v", existing)
	}
	grown := recordLines(encode(t, after))
	if len(grown) != 2 {
		t.Fatalf("expected two record lines, got %v", grown)
	}
	found := false
	for _, line := range grown {
		if line == existing[0] {
			found = true
		}
	}
	if !found {
		t.Fatalf("the existing record line %q was rewritten as %v", existing[0], grown)
	}
}

func recordLines(encoded string) []string {
	var lines []string
	for _, line := range strings.Split(encoded, "\n") {
		if line == "" || strings.HasPrefix(line, " ") {
			continue
		}
		if strings.HasPrefix(line, Magic) || strings.HasPrefix(line, "owner ") ||
			strings.HasPrefix(line, "source ") || strings.HasPrefix(line, "comparison ") ||
			strings.HasPrefix(line, "note ") {
			continue
		}
		lines = append(lines, line)
	}
	return lines
}

func TestParseRejectsMalformedFiles(t *testing.T) {
	valid := encode(t, File{Owner: "o", Source: "internal/api.go", Records: []Record{lineRecord("new", 1, 4, "a")}})
	cases := map[string]string{
		"no magic":            strings.TrimPrefix(valid, Magic+" 1\n"),
		"future version":      strings.Replace(valid, Magic+" 1", Magic+" 99", 1),
		"undeclared note":     strings.Replace(valid, testComparison.alias()+" "+noteAlias("a"), testComparison.alias()+" ffffff", 1),
		"unknown record kind": strings.Replace(valid, " lines new ", " squiggle new ", 1),
		"bad side":            strings.Replace(valid, " lines new ", " lines sideways ", 1),
		"descending range":    strings.Replace(valid, "lines new 1-4", "lines new 4-1", 1),
		"noncanonical single": strings.Replace(valid, "lines new 1-4", "lines new 4-4", 1),
		"zero line":           strings.Replace(valid, "lines new 1-4", "lines new 0", 1),
	}
	for name, text := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse(strings.NewReader(text)); err == nil {
				t.Fatalf("parse accepted %s:\n%s", name, text)
			}
		})
	}
}

func TestCoalesceMergesOnlyAdjacentRunsThatShareANote(t *testing.T) {
	records := []Record{
		lineRecord("new", 1, 1, "a"), lineRecord("new", 2, 2, "a"), lineRecord("new", 3, 3, "a"),
		lineRecord("new", 5, 5, "a"),
		lineRecord("new", 4, 4, "b"),
		lineRecord("old", 1, 1, "a"), lineRecord("old", 2, 2, "a"),
	}
	merged := Coalesce(records)
	got := map[string]bool{}
	for _, record := range merged {
		got[record.Side+" "+record.Note+" "+encodeRange(record.Start, record.End)] = true
	}
	want := []string{"new a 1-3", "new a 5", "new b 4", "old a 1-2"}
	if len(merged) != len(want) {
		t.Fatalf("coalesced to %d records, want %d: %v", len(merged), len(want), got)
	}
	for _, expected := range want {
		if !got[expected] {
			t.Fatalf("missing %q in %v", expected, got)
		}
	}
}

// Coalescing is only reversible because every integer inside a produced range
// was an owned atom. Expanding a coalesced file at Exact granularity must
// return exactly the references it started from.
func TestCoalescingIsReversibleAtExactGranularity(t *testing.T) {
	original := []Record{}
	for _, line := range []int{1, 2, 3, 7, 8, 20} {
		original = append(original, lineRecord("new", line, line, "why"))
	}
	file := File{Owner: "o", Source: "internal/api.go", Records: Coalesce(original)}
	if len(file.Records) != 3 {
		t.Fatalf("expected 3 ranges, got %d", len(file.Records))
	}
	references, err := file.References(Exact)
	if err != nil {
		t.Fatal(err)
	}
	if len(references) != len(original) {
		t.Fatalf("expanded to %d references, want %d", len(references), len(original))
	}
	expandedLines := map[int]bool{}
	for _, reference := range references {
		record, err := FromReference(reference)
		if err != nil {
			t.Fatal(err)
		}
		if record.Start != record.End {
			t.Fatalf("exact expansion produced a range: %+v", record)
		}
		expandedLines[record.Start] = true
	}
	for _, line := range []int{1, 2, 3, 7, 8, 20} {
		if !expandedLines[line] {
			t.Fatalf("line %d was lost by the coalesce/expand round trip", line)
		}
	}
}

func TestReferencesRebuildCanonicalDiffURIs(t *testing.T) {
	file := File{Owner: "o", Source: "internal/api.go", Records: []Record{lineRecord("new", 18, 42, "why")}}
	references, err := file.References(Ranges)
	if err != nil {
		t.Fatal(err)
	}
	if len(references) != 1 {
		t.Fatalf("references = %d", len(references))
	}
	record, err := FromReference(saga.DiffReference{URI: references[0].URI, Note: references[0].Note})
	if err != nil {
		t.Fatal(err)
	}
	if record.Start != 18 || record.End != 42 || record.Side != "new" || record.Path != "internal/api.go" {
		t.Fatalf("rebuilt reference lost its selector: %+v", record)
	}
	if record.Comparison != testComparison {
		t.Fatalf("rebuilt reference lost its comparison identity: %+v", record.Comparison)
	}
}
