package diffuri

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestRoundTripAndRangeMatch(t *testing.T) {
	selector := Reference{Repository: "https://example.test/acme/app.git", Base: "aaa", Head: "bbb", Kind: "line", Path: "api/handler.go", Side: "new", Start: 10, End: 20}
	value, err := Build(selector)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := Parse(value)
	if err != nil {
		t.Fatal(err)
	}
	if !Matches(parsed, Reference{Repository: selector.Repository, Base: "aaa", Head: "bbb", Kind: "line", Path: "api/handler.go", Side: "new", Start: 14, End: 14}) {
		t.Fatalf("range URI should match contained atom: %s", value)
	}
	if Matches(parsed, Reference{Repository: selector.Repository, Base: "aaa", Head: "ccc", Kind: "line", Path: "api/handler.go", Side: "new", Start: 14, End: 14}) {
		t.Fatal("different immutable head must not match")
	}
}

func TestRejectsRelativeRepository(t *testing.T) {
	_, err := Build(Reference{Repository: "../repo", Base: "aaa", Head: "bbb", Kind: "line", Path: "x", Side: "new", Start: 1, End: 1})
	if err == nil {
		t.Fatal("expected relative repository to fail")
	}
}

func TestFileURIRoundTrip(t *testing.T) {
	reference := Reference{Repository: "https://example.test/acme/app.git", Base: "aaa", Head: "bbb", Kind: "file", Path: "api/handler.go"}
	value, err := Build(reference)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := Parse(value)
	if err != nil {
		t.Fatal(err)
	}
	if parsed != reference {
		t.Fatalf("parsed = %#v, want %#v", parsed, reference)
	}
}

func TestLifecycleEventRoundTripsUseCanonicalShapes(t *testing.T) {
	for _, event := range []string{"add", "delete", "type-change", "mode", "binary", "modify"} {
		reference := Reference{Repository: "https://example.test/acme/app.git", Base: "aaa", Head: "bbb", Kind: "event", Event: event, Path: "path with space"}
		value, err := Build(reference)
		if err != nil {
			t.Fatalf("Build(%s): %v", event, err)
		}
		parsed, err := Parse(value)
		if err != nil || parsed != reference {
			t.Fatalf("Parse(Build(%s)) = %#v, %v", event, parsed, err)
		}
	}
	rename := Reference{Repository: "https://example.test/acme/app.git", Base: "aaa", Head: "bbb", Kind: "event", Event: "rename", OldPath: "old", NewPath: "new"}
	value, err := Build(rename)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := Parse(value)
	if err != nil || parsed != rename {
		t.Fatalf("rename round trip = %#v, %v", parsed, err)
	}
}

func TestParseRejectsAmbiguousUnknownAndNoncanonicalInputs(t *testing.T) {
	canonical, err := Build(Reference{Repository: "https://example.test/acme/app.git", Base: "aaa", Head: "bbb", Kind: "line", Path: "api/x.go", Side: "new", Start: 1, End: 1})
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"duplicate":          canonical + "&path=other.go",
		"unknown":            canonical + "&surprise=true",
		"fragment":           canonical + "#fragment",
		"noncanonical hex":   strings.Replace(canonical, "%2F", "%2f", 1),
		"noncanonical order": "saga-diff://v1/line?repository=https%3A%2F%2Fexample.test%2Facme%2Fapp.git&base=aaa&head=bbb&path=api%2Fx.go&side=new&start=1&end=1",
	}
	for name, value := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse(value); err == nil {
				t.Fatalf("Parse accepted %q", value)
			}
		})
	}
}

func TestBuildCanonicalizesRepositoryAndRemovesCredentials(t *testing.T) {
	value, err := Build(Reference{Repository: "HTTPS://user:secret@EXAMPLE.TEST/acme/../acme/app.git/", Base: "aaa", Head: "bbb", Kind: "file", Path: "app.go"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(value, "user") || strings.Contains(value, "secret") {
		t.Fatalf("canonical URI persisted repository credentials: %s", value)
	}
	parsed, err := Parse(value)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Repository != "https://example.test/acme/app.git" {
		t.Fatalf("repository = %q", parsed.Repository)
	}
}

func TestRejectsExtraneousEventParameters(t *testing.T) {
	_, err := Build(Reference{Repository: "https://example.test/a.git", Base: "a", Head: "b", Kind: "event", Event: "rename", Path: "new", OldPath: "old", NewPath: "new"})
	if err == nil {
		t.Fatal("rename accepted an extraneous path")
	}
	_, err = Build(Reference{Repository: "https://example.test/a.git", Base: "a", Head: "b", Kind: "event", Event: "add", Path: "new", NewPath: "new"})
	if err == nil {
		t.Fatal("add accepted an extraneous new_path")
	}
}

func TestRejectsNoncanonicalRepositoryPaths(t *testing.T) {
	for _, path := range []string{"/absolute.go", "../escape.go", "a/../b.go", "dir//file.go", "C:/file.go"} {
		_, err := Build(Reference{Repository: "https://example.test/a.git", Base: "a", Head: "b", Kind: "file", Path: path})
		if err == nil {
			t.Errorf("Build accepted path %q", path)
		}
	}
}

func TestFileRepositoryPathRoundTrip(t *testing.T) {
	want, err := filepath.Abs(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	repository, err := FileRepository(want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := RepositoryFilePath(repository)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Clean(got) != filepath.Clean(want) {
		t.Fatalf("file repository round trip = %q, want %q (%s)", got, want, repository)
	}
}
