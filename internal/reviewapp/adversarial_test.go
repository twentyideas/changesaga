package reviewapp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/twentyideas/changesaga/internal/querytest"
	"github.com/twentyideas/changesaga/internal/saga"
)

func TestDocumentedReadBounds(t *testing.T) {
	if DefaultPageLimit != 100 || MaxPageLimit != 1000 {
		t.Fatalf("page limits = %d/%d, want 100/1000", DefaultPageLimit, MaxPageLimit)
	}
	if DefaultFragmentLimit != 256<<10 || MaxFragmentLimit != 1<<20 {
		t.Fatalf("fragment limits = %d/%d, want 256KiB/1MiB", DefaultFragmentLimit, MaxFragmentLimit)
	}
}

func TestSeparateRepositoriesLargeAndActiveContentAreBoundedAndInert(t *testing.T) {
	fixture := querytest.New(t)
	largeTarget := fixture.AddLargeFragment(MaxFragmentLimit + 1)
	htmlTarget, svgTarget := fixture.AddActiveFragments()
	fixture.AddEscapingAssetSymlink()
	before := fixture.State()

	session, err := Open(context.Background(), OpenOptions{SagaRoot: fixture.SagaRoot, SourceDir: fixture.SourceDir})
	if err != nil {
		t.Fatal(err)
	}
	large, err := session.ReadFragment(context.Background(), FragmentQuery{Target: largeTarget})
	if err != nil || large.Content.Encoding != "utf-8" || len(large.Content.Data) != DefaultFragmentLimit || large.Content.NextOffset == nil {
		t.Fatalf("default large chunk = %#v, err=%v", large.Content, err)
	}
	maximum, err := session.ReadFragment(context.Background(), FragmentQuery{Target: largeTarget, Limit: MaxFragmentLimit})
	if err != nil || len(maximum.Content.Data) != MaxFragmentLimit || maximum.Content.NextOffset == nil {
		t.Fatalf("maximum large chunk = %#v, err=%v", maximum.Content, err)
	}
	final, err := session.ReadFragment(context.Background(), FragmentQuery{Target: largeTarget, Offset: MaxFragmentLimit + 1})
	if err != nil || final.Content.Data != "" || final.Content.NextOffset != nil {
		t.Fatalf("EOF chunk = %#v, err=%v", final.Content, err)
	}
	_, err = session.ReadFragment(context.Background(), FragmentQuery{Target: largeTarget, Limit: MaxFragmentLimit + 1})
	assertCode(t, err, CodeInvalidArgument)

	html, err := session.ReadFragment(context.Background(), FragmentQuery{Target: htmlTarget})
	if err != nil || html.Content.Encoding != "utf-8" || !strings.Contains(html.Content.Data, "<script>") {
		t.Fatalf("HTML was not returned as inert UTF-8: %#v, err=%v", html.Content, err)
	}
	svg, err := session.ReadFragment(context.Background(), FragmentQuery{Target: svgTarget})
	if err != nil || svg.Content.Encoding != "base64" {
		t.Fatalf("SVG encoding = %#v, err=%v", svg.Content, err)
	}
	decoded, err := base64.StdEncoding.DecodeString(svg.Content.Data)
	if err != nil || !strings.Contains(string(decoded), "<script>") {
		t.Fatalf("SVG bytes were not preserved inertly: %q, err=%v", decoded, err)
	}
	overview, err := session.ReadFragment(context.Background(), FragmentQuery{Target: querytest.OverviewTarget})
	if err != nil {
		t.Fatal(err)
	}
	for _, asset := range overview.Assets {
		if asset.Name == "leak.txt" {
			t.Fatalf("escaping asset symlink was exposed: %#v", overview.Assets)
		}
	}
	fixture.AssertUnchanged(before)
}

func TestTextChunksRespectUTF8ByteBoundaries(t *testing.T) {
	fixture := newServiceFixture(t)
	ctx := context.Background()
	chunk, err := fixture.session.ReadFragment(ctx, FragmentQuery{Target: fixture.fragment, Limit: 6})
	if err != nil || !utf8.ValidString(chunk.Content.Data) || chunk.Content.NextOffset == nil || *chunk.Content.NextOffset != 5 {
		t.Fatalf("boundary-safe chunk = %#v, err=%v", chunk.Content, err)
	}
	_, err = fixture.session.ReadFragment(ctx, FragmentQuery{Target: fixture.fragment, Offset: 6, Limit: 4})
	assertCode(t, err, CodeInvalidArgument)
}

func TestCursorsAreStableBoundAndIntegrityChecked(t *testing.T) {
	fixture := newServiceFixture(t)
	ctx := context.Background()
	root := saga.SagaTarget("query-test")
	first, err := fixture.session.Children(ctx, ChildrenQuery{Parent: root, Limit: 1})
	if err != nil || first.Page.NextCursor == nil {
		t.Fatalf("first page = %#v, err=%v", first, err)
	}
	cursor := *first.Page.NextCursor

	reopened, err := Open(ctx, OpenOptions{SagaRoot: fixture.root, SourceDir: fixture.repo})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.Children(ctx, ChildrenQuery{Parent: root, Limit: 1, Cursor: cursor}); err != nil {
		t.Fatalf("cursor did not survive an unchanged reopen: %v", err)
	}
	_, err = reopened.Gaps(ctx, GapQuery{Cursor: cursor})
	assertCode(t, err, CodeInvalidArgument)

	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		t.Fatal(err)
	}
	var token map[string]any
	if err := json.Unmarshal(raw, &token); err != nil {
		t.Fatal(err)
	}
	token["offset"] = float64(0)
	raw, _ = json.Marshal(token)
	tampered := base64.RawURLEncoding.EncodeToString(raw)
	_, err = reopened.Children(ctx, ChildrenQuery{Parent: root, Limit: 1, Cursor: tampered})
	assertCode(t, err, CodeInvalidArgument)
}

func TestInvalidAndAmbiguousSagasFailBeforeQueries(t *testing.T) {
	for _, testCase := range []struct {
		name  string
		alter func(*querytest.Fixture)
	}{
		{name: "invalid manifest", alter: (*querytest.Fixture).MakeInvalidManifest},
		{name: "ambiguous target", alter: (*querytest.Fixture).AddAmbiguousTargets},
		{name: "escaping entrypoint", alter: func(fixture *querytest.Fixture) { fixture.AddEscapingEntrypointSymlink() }},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := querytest.New(t)
			testCase.alter(fixture)
			_, err := Open(context.Background(), OpenOptions{SagaRoot: fixture.SagaRoot, SourceDir: fixture.SourceDir})
			assertCode(t, err, CodeInvalidSaga)
		})
	}
}

func TestMissingSagaErrorDoesNotExposeAbsoluteRoot(t *testing.T) {
	missing := t.TempDir() + "/private/missing.saga"
	_, err := Open(context.Background(), OpenOptions{SagaRoot: missing})
	assertCode(t, err, CodeNotFound)
	encoded, marshalErr := json.Marshal(AsError(err))
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	if strings.Contains(string(encoded), missing) {
		t.Fatalf("error leaked absolute saga root: %s", encoded)
	}
}
