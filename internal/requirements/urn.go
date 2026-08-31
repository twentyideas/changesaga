package requirements

import (
	"fmt"
	"strings"

	"github.com/twentyideas/changesaga/internal/livingid"
)

func storyURN(sagaID, storyID string) (string, error) {
	return livingid.Story(sagaID, storyID)
}

func revisionURN(sagaID, storyID, revisionID string) (string, error) {
	return livingid.Revision(sagaID, storyID, revisionID)
}

func criterionURN(sagaID, storyID, criterionID string) (string, error) {
	return livingid.Criterion(sagaID, storyID, criterionID)
}

func citationURN(sagaID, citationID string) (string, error) {
	return livingid.Citation(sagaID, citationID)
}

func relationURN(sagaID, relationID string) (string, error) {
	return livingid.Relation(sagaID, relationID)
}

// StoryEventURN returns the canonical name of an immutable lifecycle event.
// Event URNs are requirements-owned and deliberately do not require expanding
// the shared livingid parser during this merge-safe implementation slice.
func StoryEventURN(sagaID, storyID, eventID string) (string, error) {
	for name, value := range map[string]string{"saga": sagaID, "story": storyID, "event": eventID} {
		if !livingid.ValidID(value) {
			return "", fmt.Errorf("%s ID is not a stable identifier", name)
		}
	}
	return fmt.Sprintf("urn:change-saga:%s:story:%s:event:%s", sagaID, storyID, eventID), nil
}

type endpointKind string

const (
	endpointStory        endpointKind = "story"
	endpointCriterion    endpointKind = "criterion"
	endpointDesign       endpointKind = "design"
	endpointWorkItem     endpointKind = "work-item"
	endpointClaim        endpointKind = "claim"
	endpointVerification endpointKind = "verification"
	endpointCitation     endpointKind = "citation"
	endpointRelation     endpointKind = "relation"
)

type endpoint struct {
	Kind       endpointKind
	SagaID     string
	ID         string
	StoryID    string
	DesignKind livingid.DesignKind
}

func parseEndpoint(value string) (endpoint, error) {
	if ref, err := livingid.Parse(value); err == nil {
		ep := endpoint{SagaID: ref.SagaID, ID: ref.ID, StoryID: ref.ParentID, DesignKind: ref.DesignKind}
		switch ref.Kind {
		case livingid.KindStory:
			ep.Kind = endpointStory
		case livingid.KindCriterion:
			ep.Kind = endpointCriterion
		case livingid.KindDesign:
			ep.Kind = endpointDesign
		case livingid.KindWorkItem:
			ep.Kind = endpointWorkItem
		case livingid.KindCitation:
			ep.Kind = endpointCitation
		case livingid.KindRelation:
			ep.Kind = endpointRelation
		default:
			return endpoint{}, fmt.Errorf("unsupported relation endpoint kind %q", ref.Kind)
		}
		return ep, nil
	}

	parts := strings.Split(value, ":")
	if len(parts) != 5 || parts[0] != "urn" || parts[1] != "change-saga" || !livingid.ValidID(parts[2]) || !livingid.ValidID(parts[4]) {
		return endpoint{}, fmt.Errorf("endpoint must be a canonical living-Saga URN")
	}
	switch parts[3] {
	case "claim":
		return endpoint{Kind: endpointClaim, SagaID: parts[2], ID: parts[4]}, nil
	case "verification":
		return endpoint{Kind: endpointVerification, SagaID: parts[2], ID: parts[4]}, nil
	default:
		return endpoint{}, fmt.Errorf("unsupported relation endpoint kind %q", parts[3])
	}
}

func parseStoryRevision(value, sagaID string) (storyID string, err error) {
	ref, err := livingid.Parse(value)
	if err != nil || ref.Kind != livingid.KindRevision || ref.SagaID != sagaID || (ref.ParentKind != "" && ref.ParentKind != livingid.KindStory) {
		return "", fmt.Errorf("must be a canonical story revision URN in saga %q", sagaID)
	}
	return ref.ParentID, nil
}

func parseStoryEvent(value, sagaID, storyID string) (string, error) {
	parts := strings.Split(value, ":")
	if len(parts) != 7 || parts[0] != "urn" || parts[1] != "change-saga" || parts[2] != sagaID || parts[3] != "story" || parts[4] != storyID || parts[5] != "event" || !livingid.ValidID(parts[6]) {
		return "", fmt.Errorf("must be a canonical lifecycle event URN for story %q", storyID)
	}
	canonical, _ := StoryEventURN(sagaID, storyID, parts[6])
	if canonical != value {
		return "", fmt.Errorf("lifecycle event URN is not canonical")
	}
	return parts[6], nil
}
