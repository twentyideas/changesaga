package saga

import (
	"fmt"
	"sort"
	"strings"
)

// ValidateReviewerIdentity validates explicit provenance on a newly recorded
// review. A nil identity is accepted for compatibility with legacy records.
func ValidateReviewerIdentity(reviewer *ReviewerIdentity) error {
	if reviewer == nil {
		return nil
	}
	switch reviewer.Kind {
	case "human":
		if strings.TrimSpace(reviewer.Name) != "" || strings.TrimSpace(reviewer.Agent) != "" || strings.TrimSpace(reviewer.Model) != "" {
			return fmt.Errorf("human reviewer identity cannot include an AI reviewer name, agent, or model")
		}
	case "ai":
		if strings.TrimSpace(reviewer.Name) == "" || strings.TrimSpace(reviewer.Agent) == "" || strings.TrimSpace(reviewer.Model) == "" {
			return fmt.Errorf("AI reviewer identity requires reviewer name, agent, and model")
		}
	default:
		return fmt.Errorf("reviewer kind must be human or ai")
	}
	return nil
}

// CurrentReviews returns the latest event for every distinct reviewer persona.
// Git attribution identifies the author; reviewer metadata distinguishes that
// author's direct decision from decisions made through individual AI agents.
// An open or closed event retracts only the matching persona's decision.
func CurrentReviews(reviews []Review) []Review {
	latest := make(map[string]Review)
	for _, review := range reviews {
		key := ReviewIdentityKey(review)
		previous, ok := latest[key]
		if !ok || earlierReview(previous, review) {
			latest[key] = review
		}
	}
	result := make([]Review, 0, len(latest))
	for _, review := range latest {
		if review.State == "approved" || review.State == "rejected" {
			result = append(result, review)
		}
	}
	sort.Slice(result, func(i, j int) bool { return earlierReview(result[i], result[j]) })
	return result
}

// AggregateReviewState is deliberately conservative: one current rejection
// wins, otherwise one or more approvals produce approved, and no decisions is
// unreviewed. The individual decisions remain available to show who approved.
func AggregateReviewState(reviews []Review) string {
	approved := false
	for _, review := range CurrentReviews(reviews) {
		if review.State == "rejected" {
			return "rejected"
		}
		approved = approved || review.State == "approved"
	}
	if approved {
		return "approved"
	}
	return ""
}

// ReviewIdentityKey identifies a reviewer persona without trusting the legacy
// payload author. Read surfaces populate AttributionIdentity from Git; loaders
// that have not resolved Git yet fall back to the display author or local
// attribution bucket while still separating human, AI, and legacy personas.
func ReviewIdentityKey(review Review) string {
	author := strings.ToLower(strings.TrimSpace(review.AttributionIdentity))
	if author == "" {
		author = strings.ToLower(strings.TrimSpace(review.Author))
	}
	if author == "" {
		author = "unattributed"
	}
	if review.Reviewer == nil {
		return author + "\x00unspecified"
	}
	return strings.Join([]string{
		author,
		strings.ToLower(strings.TrimSpace(review.Reviewer.Kind)),
		strings.ToLower(strings.TrimSpace(review.Reviewer.Name)),
		strings.ToLower(strings.TrimSpace(review.Reviewer.Agent)),
		strings.ToLower(strings.TrimSpace(review.Reviewer.Model)),
	}, "\x00")
}

func earlierReview(left, right Review) bool {
	if left.CreatedAt.Equal(right.CreatedAt) {
		return left.ID < right.ID
	}
	return left.CreatedAt.Before(right.CreatedAt)
}
