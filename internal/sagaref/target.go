package sagaref

import (
	"fmt"
	"strings"

	"github.com/twentyideas/changesaga/internal/livingid"
)

// TargetKind identifies a stable target shape supported by cross-Saga
// references.
type TargetKind string

const (
	TargetStory        TargetKind = "story"
	TargetCriterion    TargetKind = "criterion"
	TargetPrototype    TargetKind = "prototype"
	TargetChapter      TargetKind = "chapter"
	TargetSection      TargetKind = "section"
	TargetFragment     TargetKind = "fragment"
	TargetLandmark     TargetKind = "landmark"
	TargetWorkItem     TargetKind = "work-item"
	TargetClaim        TargetKind = "claim"
	TargetVerification TargetKind = "verification"
)

// Target is the parsed identity embedded in Reference.TargetURN. ParentID is
// the story ID for a criterion and the fragment ID for a landmark.
type Target struct {
	SagaID   string
	Kind     TargetKind
	ID       string
	ParentID string
}

// ParseTarget validates a canonical stable target URN.
func ParseTarget(value string) (Target, error) {
	parts := strings.Split(value, ":")
	if len(parts) != 5 && len(parts) != 7 || len(parts) >= 2 && (parts[0] != "urn" || parts[1] != "change-saga") {
		return Target{}, fmt.Errorf("target must be a canonical supported living-Saga URN")
	}
	if !livingid.ValidID(parts[2]) {
		return Target{}, fmt.Errorf("target Saga ID is not a stable identifier")
	}
	target := Target{SagaID: parts[2]}
	if len(parts) == 5 {
		target.Kind = TargetKind(parts[3])
		target.ID = parts[4]
		switch target.Kind {
		case TargetStory, TargetPrototype, TargetChapter, TargetSection, TargetFragment, TargetWorkItem, TargetClaim, TargetVerification:
		default:
			return Target{}, fmt.Errorf("unsupported cross-Saga target kind %q", parts[3])
		}
	} else {
		target.ParentID, target.ID = parts[4], parts[6]
		switch {
		case parts[3] == "story" && parts[5] == "criterion":
			target.Kind = TargetCriterion
		case parts[3] == "fragment" && parts[5] == "landmark":
			target.Kind = TargetLandmark
		default:
			return Target{}, fmt.Errorf("unsupported nested cross-Saga target shape")
		}
		if !livingid.ValidID(target.ParentID) {
			return Target{}, fmt.Errorf("target parent ID is not a stable identifier")
		}
	}
	if !livingid.ValidID(target.ID) {
		return Target{}, fmt.Errorf("target ID is not a stable identifier")
	}
	if target.URN() != value {
		return Target{}, fmt.Errorf("target URN is not canonical")
	}
	return target, nil
}

// URN returns target's canonical stable URN, or an empty string when target is
// not a supported shape.
func (target Target) URN() string {
	if !livingid.ValidID(target.SagaID) || !livingid.ValidID(target.ID) {
		return ""
	}
	base := "urn:change-saga:" + target.SagaID + ":"
	switch target.Kind {
	case TargetStory, TargetPrototype, TargetChapter, TargetSection, TargetFragment, TargetWorkItem, TargetClaim, TargetVerification:
		if target.ParentID != "" {
			return ""
		}
		return base + string(target.Kind) + ":" + target.ID
	case TargetCriterion:
		if !livingid.ValidID(target.ParentID) {
			return ""
		}
		return base + "story:" + target.ParentID + ":criterion:" + target.ID
	case TargetLandmark:
		if !livingid.ValidID(target.ParentID) {
			return ""
		}
		return base + "fragment:" + target.ParentID + ":landmark:" + target.ID
	default:
		return ""
	}
}
