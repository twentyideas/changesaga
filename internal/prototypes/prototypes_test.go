package prototypes

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var testTime = time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)

func TestPrototypeCapabilityIsOptional(t *testing.T) {
	doc, err := Load(newSaga(t), "test")
	if err != nil {
		t.Fatal(err)
	}
	story := "urn:change-saga:test:story:optional"
	projection := Compose(doc, CompositionInputs{Stories: []StoryInput{{URN: story, CurrentRevision: story + ":revision:r1", PrototypeRequired: true}}})
	if doc.Adopted || projection.Capability != "not_applicable" || projection.Stories[0].Status != "not_applicable" || len(projection.Gaps) != 0 {
		t.Fatalf("optional capability projection = %#v", projection)
	}
}

func TestLoadRejectsSymlinkedCapabilityBoundary(t *testing.T) {
	root := newSaga(t)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "___requirements")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := Load(root, "test"); err == nil || !strings.Contains(err.Error(), "real directory") {
		t.Fatalf("symlink boundary error = %v", err)
	}
}

func TestHTMLPrototypeIsImmutableDigestVerifiedPackage(t *testing.T) {
	root := newSaga(t)
	source := filepath.Join(t.TempDir(), "checkout")
	mustMkdir(t, source)
	mustWrite(t, filepath.Join(source, "index.html"), "<!doctype html><button id=buy>Buy</button>")
	mustWrite(t, filepath.Join(source, "app.js"), "document.body.dataset.ready = 'yes'")

	created, err := AddHTML(root, "test", AddHTMLInput{ID: "checkout", RevisionID: "r1", Title: "Checkout", State: StateReady, SourcePath: source, CreatedAt: testTime})
	if err != nil {
		t.Fatal(err)
	}
	if created.URN != "urn:change-saga:test:prototype:checkout" {
		t.Fatalf("urn = %q", created.URN)
	}
	doc, err := Load(root, "test")
	if err != nil {
		t.Fatal(err)
	}
	if !doc.Adopted || len(doc.Prototypes) != 1 || doc.Prototypes[0].CurrentRevision == nil {
		t.Fatalf("loaded document = %#v", doc)
	}
	revision := doc.Prototypes[0].CurrentRevision
	if revision.Source.Kind != SourceHTML || revision.Source.Entrypoint != "html/index.html" || !digestPattern.MatchString(revision.Source.ContentDigest) {
		t.Fatalf("source = %#v", revision.Source)
	}

	// The authored source is not a live dependency after the immutable copy.
	mustWrite(t, filepath.Join(source, "index.html"), "changed outside the saga")
	if _, err := Load(root, "test"); err != nil {
		t.Fatalf("external source change affected saga: %v", err)
	}

	packaged := filepath.Join(root, filepath.FromSlash(revisionPath("checkout", "r1")), "html", "index.html")
	mustWrite(t, packaged, "tampered")
	if _, err := Load(root, "test"); err == nil || !strings.Contains(err.Error(), "digest mismatch") {
		t.Fatalf("tamper error = %v", err)
	}
}

func TestExternalAndAllowlistedEmbedSources(t *testing.T) {
	root := newSaga(t)
	if _, err := AddExternal(root, "test", AddExternalInput{ID: "figma-link", RevisionID: "r1", Title: "Figma", State: StateDraft, URL: "https://www.figma.com/file/abc", CreatedAt: testTime}); err != nil {
		t.Fatal(err)
	}
	allowlist := &ProviderAllowlist{Provider: "figma", EmbedOrigin: "https://embed.figma.com", Sandbox: []string{"allow-scripts", "allow-same-origin"}, Permissions: []string{"fullscreen"}}
	if _, err := AddExternal(root, "test", AddExternalInput{ID: "figma-embed", RevisionID: "r1", Title: "Embedded Figma", State: StateReady, URL: "https://www.figma.com/file/abc", EmbedURL: "https://embed.figma.com/proto/abc", Allowlist: allowlist, CreatedAt: testTime}); err != nil {
		t.Fatal(err)
	}
	doc, err := Load(root, "test")
	if err != nil {
		t.Fatal(err)
	}
	if got := doc.Prototypes[0].CurrentRevision.Source.FallbackURL; got != "https://www.figma.com/file/abc" {
		t.Fatalf("fallback = %q", got)
	}

	bad := *allowlist
	bad.EmbedOrigin = "https://attacker.example"
	_, err = AddExternal(root, "test", AddExternalInput{ID: "bad", RevisionID: "r1", Title: "Bad", State: StateReady, URL: "https://safe.example/fallback", EmbedURL: "https://embed.figma.com/proto/abc", Allowlist: &bad, CreatedAt: testTime})
	if err == nil || !strings.Contains(err.Error(), "exactly match") {
		t.Fatalf("origin error = %v", err)
	}
	_, err = AddExternal(root, "test", AddExternalInput{ID: "implicit", RevisionID: "r1", Title: "Implicit", State: StateReady, URL: "https://safe.example/fallback", EmbedURL: "https://embed.figma.com/proto/abc", CreatedAt: testTime})
	if err == nil || !strings.Contains(err.Error(), "explicit allowlist") {
		t.Fatalf("allowlist error = %v", err)
	}
}

func TestUnresolvedAnnotationsAreQualityGapsUntilComposition(t *testing.T) {
	root := newSaga(t)
	prototype := "urn:change-saga:test:prototype:checkout"
	story := "urn:change-saga:test:story:buyer"
	storyRevision := story + ":revision:s1"
	prototypeRevision := prototype + ":revision:r1"
	annotation, err := AddAnnotation(root, "test", AddAnnotationInput{ID: "buy-button", Prototype: prototype, Target: story, Rationale: "Shows the primary action.", PrototypeRevision: prototypeRevision, StoryRevision: storyRevision, Selector: Selector{Kind: SelectorElement, ElementID: "buy"}, CreatedAt: testTime})
	if err != nil {
		t.Fatalf("annotation should not require either endpoint: %v", err)
	}
	doc, err := Load(root, "test")
	if err != nil {
		t.Fatalf("unresolved endpoints must remain structurally valid: %v", err)
	}
	projection := Compose(doc, CompositionInputs{})
	if len(projection.Gaps) != 2 || projection.Annotations[0].Current {
		t.Fatalf("unresolved projection = %#v", projection)
	}

	source := filepath.Join(t.TempDir(), "prototype.html")
	mustWrite(t, source, "<button id=buy>Buy</button>")
	if _, err := AddHTML(root, "test", AddHTMLInput{ID: "checkout", RevisionID: "r1", Title: "Checkout", State: StateReady, SourcePath: source, CreatedAt: testTime}); err != nil {
		t.Fatal(err)
	}
	doc, err = Load(root, "test")
	if err != nil {
		t.Fatal(err)
	}
	projection = Compose(doc, CompositionInputs{Stories: []StoryInput{{URN: story, CurrentRevision: storyRevision, PrototypeRequired: true}}})
	if !projection.Annotations[0].Current || projection.Prototypes[0].Status != "linked" || projection.Stories[0].Status != "covered" || len(projection.Gaps) != 0 {
		t.Fatalf("resolved projection = %#v", projection)
	}
	if projection.Prototypes[0].Annotations[0] != annotation.URN || projection.Stories[0].Annotations[0] != annotation.URN {
		t.Fatalf("relationship was not projected bidirectionally: %#v", projection)
	}
}

func TestRevisionPinsBecomeStaleWithoutRetargeting(t *testing.T) {
	root := newSaga(t)
	prototype := "urn:change-saga:test:prototype:flow"
	if _, err := AddExternal(root, "test", AddExternalInput{ID: "flow", RevisionID: "r1", Title: "Flow", State: StateReady, URL: "https://example.com/flow", CreatedAt: testTime}); err != nil {
		t.Fatal(err)
	}
	story := "urn:change-saga:test:story:buyer"
	if _, err := AddAnnotation(root, "test", AddAnnotationInput{ID: "flow-link", Prototype: prototype, Target: story, Rationale: "Shows flow.", PrototypeRevision: prototype + ":revision:r1", StoryRevision: story + ":revision:s1", Selector: Selector{Kind: SelectorProvider, ProviderID: "node-1"}, CreatedAt: testTime}); err != nil {
		t.Fatal(err)
	}
	if _, err := Revise(root, "test", ReviseInput{Prototype: prototype, ID: "r2", Parents: []string{prototype + ":revision:r1"}, Title: "Flow v2", State: StateReady, Source: Source{Kind: SourceExternal, URL: "https://example.com/flow-v2"}, CreatedAt: testTime.Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	doc, err := Load(root, "test")
	if err != nil {
		t.Fatal(err)
	}
	projection := Compose(doc, CompositionInputs{Stories: []StoryInput{{URN: story, CurrentRevision: story + ":revision:s2", PrototypeRequired: true}}})
	reasons := strings.Join(projection.Annotations[0].StaleReasons, ",")
	if projection.Annotations[0].Current || !strings.Contains(reasons, "prototype revision changed") || !strings.Contains(reasons, "story revision changed") {
		t.Fatalf("stale projection = %#v", projection.Annotations[0])
	}
}

func TestRepositoryStylesArePinnedAndFailClosed(t *testing.T) {
	root := newSaga(t)
	html := filepath.Join(t.TempDir(), "prototype.html")
	mustWrite(t, html, "<button class=button>Buy</button>")
	if _, err := AddHTML(root, "test", AddHTMLInput{ID: "styled", RevisionID: "r1", Title: "Styled", State: StateReady, SourcePath: html, CreatedAt: testTime}); err != nil {
		t.Fatal(err)
	}
	repository := t.TempDir()
	mustMkdir(t, filepath.Join(repository, "styles"))
	cssPath := filepath.Join(repository, "styles", "tokens.css")
	mustWrite(t, cssPath, ":root { --brand: #06c; } .button { color: var(--brand); }")
	prototype := "urn:change-saga:test:prototype:styled"
	if _, err := AddStyle(root, "test", AddStyleInput{Prototype: prototype, ID: "r2", RepositoryRoot: repository, Path: "styles/tokens.css", CustomProperties: []string{"--brand"}, Roles: []StyleRole{{Role: "primary-action", Class: "button"}}, CreatedAt: testTime.Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	doc, err := LoadWithOptions(root, "test", LoadOptions{RepositoryRoot: repository})
	if err != nil {
		t.Fatal(err)
	}
	style := doc.Prototypes[0].CurrentRevision.Styles[0]
	if style.Stale || !digestPattern.MatchString(style.Digest) {
		t.Fatalf("style = %#v", style)
	}
	if data, err := ReadPinnedStyle(repository, style); err != nil || !strings.Contains(string(data), "--brand") {
		t.Fatalf("read pinned style = %q, %v", data, err)
	}
	mustWrite(t, cssPath, ":root { --brand: red; }")
	if _, err := ReadPinnedStyle(repository, style); err == nil || !strings.Contains(err.Error(), "digest changed") {
		t.Fatalf("stale pinned read = %v", err)
	}
	doc, err = LoadWithOptions(root, "test", LoadOptions{RepositoryRoot: repository})
	if err != nil {
		t.Fatal(err)
	}
	if !doc.Prototypes[0].CurrentRevision.Styles[0].Stale {
		t.Fatal("changed stylesheet was not marked stale")
	}
	if _, err := RefreshRepositoryStyle(root, "test", AddStyleInput{Prototype: prototype, ID: "r3", RepositoryRoot: repository, Path: "styles/tokens.css", CustomProperties: []string{"--brand"}, Roles: []StyleRole{{Role: "primary-action", Class: "button"}}, CreatedAt: testTime.Add(2 * time.Minute)}); err != nil {
		t.Fatal(err)
	}
	doc, err = LoadWithOptions(root, "test", LoadOptions{RepositoryRoot: repository})
	if err != nil || doc.Prototypes[0].CurrentRevision.Styles[0].Stale || len(doc.Prototypes[0].CurrentRevision.Styles) != 1 {
		t.Fatalf("refreshed stylesheet = %#v, %v", doc.Prototypes[0].CurrentRevision.Styles, err)
	}

	for _, attack := range []struct{ path, body string }{{"../escape.css", "body{}"}, {"styles/remote.css", "@import 'https://evil.example/x.css';"}, {"styles/fetch.css", "body{background:url(https://evil.example/x)}"}, {"styles/escaped.css", `body{background:u\72l(https://evil.example/x)}`}, {"styles/commented.css", "body{background:u/**/rl(https://evil.example/x)}"}} {
		if !strings.HasPrefix(attack.path, "..") {
			mustWrite(t, filepath.Join(repository, filepath.FromSlash(attack.path)), attack.body)
		}
		_, err := AddStyle(root, "test", AddStyleInput{Prototype: prototype, ID: strings.ReplaceAll(filepath.Base(attack.path), ".", "-"), RepositoryRoot: repository, Path: attack.path, CreatedAt: testTime.Add(2 * time.Minute)})
		if err == nil {
			t.Fatalf("unsafe stylesheet %q accepted", attack.path)
		}
	}
}

func TestAnnotationIdentityIsScopedToPrototype(t *testing.T) {
	root := newSaga(t)
	story := "urn:change-saga:test:story:shared"
	for _, prototypeID := range []string{"one", "two"} {
		prototype := "urn:change-saga:test:prototype:" + prototypeID
		if _, err := AddAnnotation(root, "test", AddAnnotationInput{ID: "same", Prototype: prototype, Target: story, Rationale: "Scoped edge.", PrototypeRevision: prototype + ":revision:r1", StoryRevision: story + ":revision:s1", Selector: Selector{Kind: SelectorRegion, Region: &Region{X: 0, Y: 0, Width: 1, Height: 1}}, CreatedAt: testTime}); err != nil {
			t.Fatal(err)
		}
	}
	doc, err := Load(root, "test")
	if err != nil || len(doc.Annotations) != 2 {
		t.Fatalf("scoped annotations = %#v, %v", doc.Annotations, err)
	}
}

func TestSelectorValidationAndSourceCompatibility(t *testing.T) {
	root := newSaga(t)
	prototype := "urn:change-saga:test:prototype:external"
	if _, err := AddExternal(root, "test", AddExternalInput{ID: "external", RevisionID: "r1", Title: "External", State: StateReady, URL: "https://example.com", CreatedAt: testTime}); err != nil {
		t.Fatal(err)
	}
	story := "urn:change-saga:test:story:story"
	_, err := AddAnnotation(root, "test", AddAnnotationInput{ID: "bad-region", Prototype: prototype, Target: story, Rationale: "Bad region.", PrototypeRevision: prototype + ":revision:r1", StoryRevision: story + ":revision:s1", Selector: Selector{Kind: SelectorRegion, Region: &Region{X: .9, Y: 0, Width: .2, Height: .2}}, CreatedAt: testTime})
	if err == nil || !strings.Contains(err.Error(), "normalized") {
		t.Fatalf("region error = %v", err)
	}
	if _, err := AddAnnotation(root, "test", AddAnnotationInput{ID: "html-selector", Prototype: prototype, Target: story, Rationale: "Incompatible after composition.", PrototypeRevision: prototype + ":revision:r1", StoryRevision: story + ":revision:s1", Selector: Selector{Kind: SelectorText, ExactText: "Hello"}, CreatedAt: testTime}); err != nil {
		t.Fatal(err)
	}
	doc, err := Load(root, "test")
	if err != nil {
		t.Fatal(err)
	}
	projection := Compose(doc, CompositionInputs{Stories: []StoryInput{{URN: story, CurrentRevision: story + ":revision:s1"}}})
	if projection.Annotations[0].Current || !strings.Contains(strings.Join(projection.Annotations[0].StaleReasons, ","), "incompatible") {
		t.Fatalf("compatibility = %#v", projection)
	}
}

func newSaga(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "saga.json"), `{"$schema":"https://changesaga.dev/schema/v3/saga.schema.json","version":3,"id":"test","title":"Test","source":{"repository":"https://example.com/repo.git","base":"main","head":"feature"}}`)
	return root
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
}
func mustWrite(t *testing.T, path, value string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(value), 0o644); err != nil {
		t.Fatal(err)
	}
}
