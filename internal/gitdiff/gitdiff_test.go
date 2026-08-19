package gitdiff

import (
	"testing"
)

func TestParseLinesAndEvents(t *testing.T) {
	patch := []byte(`diff --git a/app.go b/app.go
index 1111111..2222222 100644
--- a/app.go
+++ b/app.go
@@ -2,2 +2,2 @@
-old one
-old two
+new one
+new two
diff --git a/old.go b/new.go
similarity index 100%
rename from old.go
rename to new.go
diff --git "a/script name.sh" "b/script name.sh"
old mode 100644
new mode 100755
diff --git a/logo.png b/logo.png
index 3333333..4444444 100644
Binary files a/logo.png and b/logo.png differ
`)
	atoms, err := Parse(patch)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(atoms), 7; got != want {
		t.Fatalf("got %d atoms, want %d: %#v", got, want, atoms)
	}
	wants := []struct {
		key     string
		content string
	}{
		{"line:app.go:old:2", "old one"},
		{"line:app.go:old:3", "old two"},
		{"line:app.go:new:2", "new one"},
		{"line:app.go:new:3", "new two"},
		{"event:rename:new.go:old.go:new.go", ""},
		{"event:mode:script name.sh::", ""},
		{"event:binary:logo.png:logo.png:logo.png", ""},
	}
	for i, want := range wants {
		if atoms[i].Key != want.key || atoms[i].Content != want.content {
			t.Errorf("atom %d = %#v, want key %q content %q", i, atoms[i], want.key, want.content)
		}
	}
}

func TestIsSagaPath(t *testing.T) {
	tests := map[string]bool{
		"pr-12.saga/title.md":              true,
		"docs/reviews/pr-12.saga/a/x.json": true,
		"docs/saga/title.md":               false,
		"service.saga.go":                  false,
	}
	for path, want := range tests {
		if got := IsSagaPath(path); got != want {
			t.Errorf("IsSagaPath(%q) = %v, want %v", path, got, want)
		}
	}
}
