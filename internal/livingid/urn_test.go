package livingid

import (
	"strings"
	"testing"
)

func TestResourceURNsRoundTrip(t *testing.T) {
	tests := []struct {
		name  string
		build func() (string, error)
		want  string
		ref   Reference
	}{
		{"story", func() (string, error) { return Story("checkout", "retry") }, "urn:change-saga:checkout:story:retry", Reference{SagaID: "checkout", Kind: KindStory, ID: "retry"}},
		{"criterion", func() (string, error) { return Criterion("checkout", "retry", "idempotent") }, "urn:change-saga:checkout:story:retry:criterion:idempotent", Reference{SagaID: "checkout", Kind: KindCriterion, ParentID: "retry", ID: "idempotent"}},
		{"revision", func() (string, error) { return Revision("checkout", "retry", "rev.2") }, "urn:change-saga:checkout:story:retry:revision:rev.2", Reference{SagaID: "checkout", Kind: KindRevision, ParentID: "retry", ID: "rev.2"}},
		{"wave revision", func() (string, error) { return DefinitionRevision("checkout", KindWave, "foundation", "rev.2") }, "urn:change-saga:checkout:wave:foundation:revision:rev.2", Reference{SagaID: "checkout", Kind: KindRevision, ParentKind: KindWave, ParentID: "foundation", ID: "rev.2"}},
		{"work-item revision", func() (string, error) { return DefinitionRevision("checkout", KindWorkItem, "add-key", "rev.2") }, "urn:change-saga:checkout:work-item:add-key:revision:rev.2", Reference{SagaID: "checkout", Kind: KindRevision, ParentKind: KindWorkItem, ParentID: "add-key", ID: "rev.2"}},
		{"contract revision", func() (string, error) { return DefinitionRevision("checkout", KindContract, "key-format", "rev.2") }, "urn:change-saga:checkout:contract:key-format:revision:rev.2", Reference{SagaID: "checkout", Kind: KindRevision, ParentKind: KindContract, ParentID: "key-format", ID: "rev.2"}},
		{"citation", func() (string, error) { return Citation("checkout", "decision-1") }, "urn:change-saga:checkout:citation:decision-1", Reference{SagaID: "checkout", Kind: KindCitation, ID: "decision-1"}},
		{"relation", func() (string, error) { return Relation("checkout", "addresses-retry") }, "urn:change-saga:checkout:relation:addresses-retry", Reference{SagaID: "checkout", Kind: KindRelation, ID: "addresses-retry"}},
		{"wave", func() (string, error) { return Wave("checkout", "foundation") }, "urn:change-saga:checkout:wave:foundation", Reference{SagaID: "checkout", Kind: KindWave, ID: "foundation"}},
		{"work item", func() (string, error) { return WorkItem("checkout", "add-key") }, "urn:change-saga:checkout:work-item:add-key", Reference{SagaID: "checkout", Kind: KindWorkItem, ID: "add-key"}},
		{"dependency", func() (string, error) { return Dependency("checkout", "storage-first") }, "urn:change-saga:checkout:dependency:storage-first", Reference{SagaID: "checkout", Kind: KindDependency, ID: "storage-first"}},
		{"contract", func() (string, error) { return Contract("checkout", "key-format") }, "urn:change-saga:checkout:contract:key-format", Reference{SagaID: "checkout", Kind: KindContract, ID: "key-format"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value, err := test.build()
			if err != nil {
				t.Fatal(err)
			}
			if value != test.want {
				t.Fatalf("built URN = %q, want %q", value, test.want)
			}
			parsed, err := Parse(value)
			if err != nil {
				t.Fatal(err)
			}
			if parsed != test.ref {
				t.Fatalf("parsed reference = %#v, want %#v", parsed, test.ref)
			}
		})
	}
}

func TestDesignURNsReuseAddressablePackageTargets(t *testing.T) {
	tests := []struct {
		name string
		ref  Reference
		want string
	}{
		{"chapter", Reference{SagaID: "checkout", Kind: KindDesign, DesignKind: DesignChapter, ID: "architecture"}, "urn:change-saga:checkout:chapter:architecture"},
		{"section", Reference{SagaID: "checkout", Kind: KindDesign, DesignKind: DesignSection, ID: "storage"}, "urn:change-saga:checkout:section:storage"},
		{"fragment", Reference{SagaID: "checkout", Kind: KindDesign, DesignKind: DesignFragment, ID: "sequence"}, "urn:change-saga:checkout:fragment:sequence"},
		{"landmark", Reference{SagaID: "checkout", Kind: KindDesign, DesignKind: DesignLandmark, ParentID: "sequence", ID: "retry-edge"}, "urn:change-saga:checkout:fragment:sequence:landmark:retry-edge"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value, err := Build(test.ref)
			if err != nil {
				t.Fatal(err)
			}
			if value != test.want {
				t.Fatalf("built URN = %q, want %q", value, test.want)
			}
			parsed, err := Parse(value)
			if err != nil {
				t.Fatal(err)
			}
			if parsed != test.ref {
				t.Fatalf("parsed reference = %#v, want %#v", parsed, test.ref)
			}
		})
	}

	if _, err := Design("checkout", DesignLandmark, "retry-edge"); err == nil {
		t.Fatal("Design accepted a landmark without its fragment parent")
	}
	value, err := Landmark("checkout", "sequence", "retry-edge")
	if err != nil || value != tests[3].want {
		t.Fatalf("Landmark() = %q, %v", value, err)
	}
}

func TestBuildRejectsInvalidOrAmbiguousReferences(t *testing.T) {
	tooLong := "a" + strings.Repeat("b", 128)
	tests := []Reference{
		{SagaID: "", Kind: KindStory, ID: "story"},
		{SagaID: "bad:saga", Kind: KindStory, ID: "story"},
		{SagaID: "saga", Kind: KindStory, ID: ""},
		{SagaID: "saga", Kind: KindStory, ID: tooLong},
		{SagaID: "saga", Kind: KindCriterion, ID: "criterion"},
		{SagaID: "saga", Kind: KindRevision, ParentID: "bad:story", ID: "revision"},
		{SagaID: "saga", Kind: KindRevision, ParentKind: KindCitation, ParentID: "citation", ID: "revision"},
		{SagaID: "saga", Kind: KindCitation, ParentID: "unexpected", ID: "citation"},
		{SagaID: "saga", Kind: KindDesign, DesignKind: "diagram", ID: "design"},
		{SagaID: "saga", Kind: KindDesign, DesignKind: DesignFragment, ParentID: "unexpected", ID: "design"},
		{SagaID: "saga", Kind: KindDesign, DesignKind: DesignLandmark, ID: "landmark"},
		{SagaID: "saga", Kind: "unknown", ID: "resource"},
	}
	for _, reference := range tests {
		if value, err := Build(reference); err == nil {
			t.Errorf("Build(%#v) = %q, want error", reference, value)
		}
	}
}

func TestParseRejectsMalformedAndUnsupportedURNs(t *testing.T) {
	tests := []string{
		"",
		"URN:change-saga:saga:story:story",
		"urn:change-saga:saga:story",
		"urn:change-saga:saga:story:story:extra:value",
		"urn:change-saga:saga:criterion:criterion",
		"urn:change-saga:saga:revision:revision",
		"urn:change-saga:saga:design:design",
		"urn:change-saga:saga:story:story:landmark:value",
		"urn:change-saga:saga:fragment:fragment:criterion:value",
		"urn:change-saga:saga:claim:claim",
		"urn:change-saga:saga:story:percent%2Did",
		"urn:change-saga:bad saga:story:story",
	}
	for _, value := range tests {
		t.Run(value, func(t *testing.T) {
			if parsed, err := Parse(value); err == nil {
				t.Fatalf("Parse(%q) = %#v, want error", value, parsed)
			}
		})
	}
}

func TestValidIDMatchesExistingStableIDGrammar(t *testing.T) {
	for _, value := range []string{"a", "A0", "story.with_parts-2"} {
		if !ValidID(value) {
			t.Errorf("ValidID(%q) = false", value)
		}
	}
	for _, value := range []string{"", "-starts-with-punctuation", "contains space", "contains:colon", "é"} {
		if ValidID(value) {
			t.Errorf("ValidID(%q) = true", value)
		}
	}
}
