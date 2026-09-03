package sagaref

import "testing"

func TestSupportedTargetsRoundTrip(t *testing.T) {
	tests := []struct {
		urn  string
		want Target
	}{
		{"urn:change-saga:checkout:story:retry", Target{SagaID: "checkout", Kind: TargetStory, ID: "retry"}},
		{"urn:change-saga:checkout:story:retry:criterion:idempotent", Target{SagaID: "checkout", Kind: TargetCriterion, ParentID: "retry", ID: "idempotent"}},
		{"urn:change-saga:checkout:prototype:failure", Target{SagaID: "checkout", Kind: TargetPrototype, ID: "failure"}},
		{"urn:change-saga:checkout:chapter:architecture", Target{SagaID: "checkout", Kind: TargetChapter, ID: "architecture"}},
		{"urn:change-saga:checkout:section:storage", Target{SagaID: "checkout", Kind: TargetSection, ID: "storage"}},
		{"urn:change-saga:checkout:fragment:sequence", Target{SagaID: "checkout", Kind: TargetFragment, ID: "sequence"}},
		{"urn:change-saga:checkout:fragment:sequence:landmark:retry-edge", Target{SagaID: "checkout", Kind: TargetLandmark, ParentID: "sequence", ID: "retry-edge"}},
		{"urn:change-saga:checkout:deck:validation", Target{SagaID: "checkout", Kind: TargetDeck, ID: "validation"}},
		{"urn:change-saga:checkout:slide:reject-early", Target{SagaID: "checkout", Kind: TargetSlide, ID: "reject-early"}},
		{"urn:change-saga:checkout:slide:reject-early:item:guard", Target{SagaID: "checkout", Kind: TargetItem, ParentID: "reject-early", ID: "guard"}},
		{"urn:change-saga:checkout:work-item:resolver", Target{SagaID: "checkout", Kind: TargetWorkItem, ID: "resolver"}},
		{"urn:change-saga:checkout:claim:portable", Target{SagaID: "checkout", Kind: TargetClaim, ID: "portable"}},
		{"urn:change-saga:checkout:verification:portable-check", Target{SagaID: "checkout", Kind: TargetVerification, ID: "portable-check"}},
	}
	for _, test := range tests {
		t.Run(string(test.want.Kind), func(t *testing.T) {
			got, err := ParseTarget(test.urn)
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want || got.URN() != test.urn {
				t.Fatalf("ParseTarget(%q) = %#v (%q), want %#v", test.urn, got, got.URN(), test.want)
			}
		})
	}
}

func TestParseTargetRejectsUnsupportedOrMalformedURNs(t *testing.T) {
	tests := []string{
		"",
		"URN:change-saga:checkout:story:retry",
		"urn:change-saga:checkout:citation:source",
		"urn:change-saga:checkout:story:retry:revision:two",
		"urn:change-saga:checkout:prototype:failure:annotation:button",
		"urn:change-saga:checkout:section:one:landmark:point",
		"urn:change-saga:bad saga:fragment:flow",
		"urn:change-saga:checkout:fragment:bad%2Did",
	}
	for _, value := range tests {
		t.Run(value, func(t *testing.T) {
			if target, err := ParseTarget(value); err == nil {
				t.Fatalf("ParseTarget(%q) = %#v, want error", value, target)
			}
		})
	}
}
