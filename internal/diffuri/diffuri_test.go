package diffuri

import "testing"

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
