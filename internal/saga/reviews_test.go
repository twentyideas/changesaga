package saga

import (
	"testing"
	"time"
)

func TestCurrentReviewsKeepsOneDecisionPerAuthorAndPersona(t *testing.T) {
	base := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	human := &ReviewerIdentity{Kind: "human"}
	ai := &ReviewerIdentity{Kind: "ai", Name: "Codex 1", Agent: "codex", Model: "gpt-5.6-sol"}
	ai2 := &ReviewerIdentity{Kind: "ai", Name: "Codex 2", Agent: "codex", Model: "gpt-5.6-sol"}
	reviews := []Review{
		{ID: "1", AttributionIdentity: "git:chris@example.test", Reviewer: ai, State: "approved", CreatedAt: base},
		{ID: "2", AttributionIdentity: "git:chris@example.test", Reviewer: human, State: "approved", CreatedAt: base.Add(time.Second)},
		{ID: "3", AttributionIdentity: "git:pat@example.test", Reviewer: human, State: "rejected", CreatedAt: base.Add(2 * time.Second)},
		{ID: "4", AttributionIdentity: "git:chris@example.test", Reviewer: human, State: "open", CreatedAt: base.Add(3 * time.Second)},
		{ID: "5", AttributionIdentity: "git:chris@example.test", Reviewer: ai2, State: "approved", CreatedAt: base.Add(4 * time.Second)},
	}

	current := CurrentReviews(reviews)
	if len(current) != 3 {
		t.Fatalf("current decisions = %#v, want two named AI approvals and Pat's rejection", current)
	}
	if current[0].Reviewer == nil || current[0].Reviewer.Name != "Codex 1" || current[1].State != "rejected" || current[2].Reviewer == nil || current[2].Reviewer.Name != "Codex 2" {
		t.Fatalf("unexpected current decisions: %#v", current)
	}
	if got := AggregateReviewState(reviews); got != "rejected" {
		t.Fatalf("aggregate = %q, want rejected", got)
	}
}

func TestReviewerIdentityValidationDistinguishesHumanAndAI(t *testing.T) {
	valid := []*ReviewerIdentity{nil, {Kind: "human"}, {Kind: "ai", Name: "Codex 1", Agent: "codex", Model: "gpt-5.6-sol"}}
	for _, reviewer := range valid {
		if err := ValidateReviewerIdentity(reviewer); err != nil {
			t.Errorf("valid identity %#v: %v", reviewer, err)
		}
	}
	invalid := []*ReviewerIdentity{
		{Kind: ""},
		{Kind: "robot"},
		{Kind: "human", Model: "gpt-5.6-sol"},
		{Kind: "ai", Agent: "codex", Model: "gpt-5.6-sol"},
		{Kind: "ai", Agent: "codex"},
		{Kind: "ai", Model: "gpt-5.6-sol"},
	}
	for _, reviewer := range invalid {
		if err := ValidateReviewerIdentity(reviewer); err == nil {
			t.Errorf("invalid identity %#v was accepted", reviewer)
		}
	}
}

func TestLegacyReviewIdentityRemainsUnspecified(t *testing.T) {
	legacy := Review{ID: "legacy", AttributionIdentity: "git:reviewer@example.test", State: "approved", CreatedAt: time.Now()}
	if legacy.Reviewer != nil {
		t.Fatal("legacy review was assigned a guessed persona")
	}
	if got := CurrentReviews([]Review{legacy}); len(got) != 1 || got[0].Reviewer != nil {
		t.Fatalf("legacy decision did not remain readable and unspecified: %#v", got)
	}
}
