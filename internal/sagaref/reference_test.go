package sagaref

import (
	"context"
	"strings"
	"testing"
)

const testRevision = "0123456789abcdef0123456789abcdef01234567"

func testReference(target string) Reference {
	return Reference{
		Repository:     "https://github.com/twentyideas/checkout",
		SagaPath:       "sagas/checkout.saga",
		SagaID:         "checkout",
		Revision:       testRevision,
		TargetURN:      target,
		TrackingBranch: "feature/retry",
		ViewURL:        "https://changesaga.example/sagas/checkout?tab=design#retry",
	}
}

func TestBuildParseCanonicalRoundTrip(t *testing.T) {
	reference := testReference("urn:change-saga:checkout:story:retry:criterion:idempotent")
	reference.Repository = " HTTPS://User:secret@GitHub.COM:443/twentyideas/checkout/ "
	reference.Revision = strings.ToUpper(testRevision)
	reference.ViewURL = "https://CHANGESAGA.EXAMPLE:443/sagas/checkout?tab=design#retry"

	value, err := Build(reference)
	if err != nil {
		t.Fatal(err)
	}
	want := "saga-ref://v1/target?repository=https%3A%2F%2Fgithub.com%2Ftwentyideas%2Fcheckout&revision=" + testRevision +
		"&saga_id=checkout&saga_path=sagas%2Fcheckout.saga&target=urn%3Achange-saga%3Acheckout%3Astory%3Aretry%3Acriterion%3Aidempotent" +
		"&tracking_branch=feature%2Fretry&view_url=https%3A%2F%2Fchangesaga.example%2Fsagas%2Fcheckout%3Ftab%3Ddesign%23retry"
	if value != want {
		t.Fatalf("Build() = %q, want %q", value, want)
	}
	parsed, err := Parse(value)
	if err != nil {
		t.Fatal(err)
	}
	wantReference := testReference(reference.TargetURN)
	if parsed != wantReference {
		t.Fatalf("Parse() = %#v, want %#v", parsed, wantReference)
	}
}

func TestOptionalRefreshMetadataIsOmitted(t *testing.T) {
	reference := testReference("urn:change-saga:checkout:prototype:failure-state")
	reference.TrackingBranch = ""
	reference.ViewURL = ""
	value, err := Build(reference)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(value, "tracking_branch") || strings.Contains(value, "view_url") {
		t.Fatalf("optional fields were serialized: %s", value)
	}
	parsed, err := Parse(value)
	if err != nil || parsed != reference {
		t.Fatalf("Parse() = %#v, %v; want %#v", parsed, err, reference)
	}
}

func TestParseRejectsNonCanonicalAndMalformedReferences(t *testing.T) {
	canonical, err := Build(testReference("urn:change-saga:checkout:fragment:flow"))
	if err != nil {
		t.Fatal(err)
	}
	tests := []string{
		"",
		strings.Replace(canonical, "saga-ref://", "SAGA-REF://", 1),
		strings.Replace(canonical, "/target?", "/other?", 1),
		canonical + "#fragment",
		canonical + "&unknown=value",
		canonical + "&target=urn%3Achange-saga%3Acheckout%3Afragment%3Aflow",
		strings.Replace(canonical, "repository=", "repository=&unused=", 1),
		strings.Replace(canonical, "revision="+testRevision, "revision="+strings.ToUpper(testRevision), 1),
		strings.Replace(canonical, "repository=https%3A%2F%2Fgithub.com", "repository=https%3A%2F%2FGitHub.com%3A443", 1),
		strings.Replace(canonical, "tracking_branch=feature%2Fretry", "tracking_branch=", 1),
	}
	for _, value := range tests {
		t.Run(value, func(t *testing.T) {
			if parsed, err := Parse(value); err == nil {
				t.Fatalf("Parse(%q) = %#v, want error", value, parsed)
			}
		})
	}
}

func TestBuildRejectsStructurallyInvalidReferences(t *testing.T) {
	valid := testReference("urn:change-saga:checkout:work-item:resolver")
	tests := []struct {
		name   string
		mutate func(*Reference)
	}{
		{"relative repository", func(r *Reference) { r.Repository = "github.com/org/repo" }},
		{"repository query", func(r *Reference) { r.Repository += "?token=secret" }},
		{"absolute Saga path", func(r *Reference) { r.SagaPath = "/checkout.saga" }},
		{"unclean Saga path", func(r *Reference) { r.SagaPath = "sagas/../checkout.saga" }},
		{"non Saga directory", func(r *Reference) { r.SagaPath = "sagas/checkout" }},
		{"invalid Saga ID", func(r *Reference) { r.SagaID = "bad:id" }},
		{"symbolic revision", func(r *Reference) { r.Revision = "main" }},
		{"abbreviated revision", func(r *Reference) { r.Revision = "0123456" }},
		{"wrong target Saga", func(r *Reference) { r.TargetURN = "urn:change-saga:payments:work-item:resolver" }},
		{"unsupported target", func(r *Reference) { r.TargetURN = "urn:change-saga:checkout:citation:source" }},
		{"full branch ref", func(r *Reference) { r.TrackingBranch = "refs/heads/main" }},
		{"invalid branch", func(r *Reference) { r.TrackingBranch = "bad..branch" }},
		{"relative view URL", func(r *Reference) { r.ViewURL = "/checkout" }},
		{"authenticated view URL", func(r *Reference) { r.ViewURL = "https://user@example.com/checkout" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reference := valid
			test.mutate(&reference)
			if value, err := Build(reference); err == nil {
				t.Fatalf("Build(%#v) = %q, want error", reference, value)
			}
		})
	}
}

func TestImmutableSHA256Revision(t *testing.T) {
	reference := testReference("urn:change-saga:checkout:claim:portable-resolution")
	reference.Revision = strings.Repeat("a", 64)
	value, err := Build(reference)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(value); err != nil {
		t.Fatal(err)
	}
}

func TestCanonicalRepositoryForms(t *testing.T) {
	tests := map[string]string{
		"https://Example.COM:443/org/repo/": "https://example.com/org/repo",
		"ssh://git@Example.COM:22/org/repo": "ssh://example.com/org/repo",
		"file:///tmp/repos/../repo":         "file:///tmp/repo",
		"https://[2001:DB8::1]:443/repo":    "https://[2001:db8::1]/repo",
	}
	for input, want := range tests {
		got, err := CanonicalRepository(input)
		if err != nil || got != want {
			t.Errorf("CanonicalRepository(%q) = %q, %v; want %q", input, got, err, want)
		}
	}
}

type recordingQueryAPI struct {
	request QueryRequest
}

func (api *recordingQueryAPI) ResolveTarget(_ context.Context, request QueryRequest) (QueryResult, error) {
	api.request = request
	return QueryResult{Status: StatusResolved}, nil
}

func TestVersionedQueryBoundaryUsesPinnedIdentity(t *testing.T) {
	reference := testReference("urn:change-saga:checkout:verification:cross-saga-check")
	request, err := NewQueryRequest(reference)
	if err != nil {
		t.Fatal(err)
	}
	var api VersionedQueryAPI = &recordingQueryAPI{}
	if _, err := api.ResolveTarget(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if request.Schema != QueryAPIVersion || request.Revision != reference.Revision || request.TargetURN != reference.TargetURN || request.TrackingBranch != reference.TrackingBranch {
		t.Fatalf("query request lost pinned or refresh identity: %#v", request)
	}
	for status, want := range map[ResolutionStatus]string{
		StatusResolved: "resolved", StatusStale: "stale", StatusUnavailable: "unavailable",
	} {
		if string(status) != want {
			t.Errorf("status = %q, want %q", status, want)
		}
	}
}

func TestVersionedQueryBoundaryUsesV2ForSlideTargets(t *testing.T) {
	reference := testReference("urn:change-saga:checkout:slide:reject-early:item:guard")
	request, err := NewQueryRequest(reference)
	if err != nil {
		t.Fatal(err)
	}
	if request.Schema != SlideQueryAPIVersion || request.TargetURN != reference.TargetURN || request.Revision != reference.Revision {
		t.Fatalf("slide reference lost its v2 pinned identity: %#v", request)
	}
}
