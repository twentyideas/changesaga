// Package livingid builds and parses stable resource URNs used by living Sagas.
package livingid

import (
	"fmt"
	"regexp"
	"strings"
)

const prefix = "urn:change-saga:"

// Kind identifies a living-Saga resource class. Design targets keep their
// existing chapter, section, fragment, or landmark spelling in the URN while
// parsing to KindDesign.
type Kind string

const (
	KindStory      Kind = "story"
	KindCriterion  Kind = "criterion"
	KindRevision   Kind = "revision"
	KindCitation   Kind = "citation"
	KindRelation   Kind = "relation"
	KindDesign     Kind = "design"
	KindWave       Kind = "wave"
	KindWorkItem   Kind = "work-item"
	KindDependency Kind = "dependency"
	KindContract   Kind = "contract"
)

// DesignKind is the existing addressable package kind reused beneath the v3
// design root.
type DesignKind string

const (
	DesignChapter  DesignKind = "chapter"
	DesignSection  DesignKind = "section"
	DesignFragment DesignKind = "fragment"
	DesignLandmark DesignKind = "landmark"
)

// Reference is the structured form of a stable living-Saga URN.
//
// ParentID is the story ID for criteria and revisions, and the fragment ID for
// design landmarks. DesignKind is set only when Kind is KindDesign.
type Reference struct {
	SagaID     string
	Kind       Kind
	ID         string
	ParentID   string
	DesignKind DesignKind
}

var stableID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

// ValidID reports whether value uses the stable-ID grammar shared by Saga
// resources.
func ValidID(value string) bool {
	return stableID.MatchString(value)
}

// Build returns the canonical URN for reference.
func Build(reference Reference) (string, error) {
	if err := validateID("saga", reference.SagaID); err != nil {
		return "", err
	}
	if err := validateID(string(reference.Kind), reference.ID); err != nil {
		return "", err
	}

	base := prefix + reference.SagaID + ":"
	switch reference.Kind {
	case KindStory, KindCitation, KindRelation, KindWave, KindWorkItem, KindDependency, KindContract:
		if reference.ParentID != "" || reference.DesignKind != "" {
			return "", fmt.Errorf("%s reference cannot have a parent or design kind", reference.Kind)
		}
		return base + string(reference.Kind) + ":" + reference.ID, nil
	case KindCriterion, KindRevision:
		if err := validateID("story", reference.ParentID); err != nil {
			return "", err
		}
		if reference.DesignKind != "" {
			return "", fmt.Errorf("%s reference cannot have a design kind", reference.Kind)
		}
		return base + "story:" + reference.ParentID + ":" + string(reference.Kind) + ":" + reference.ID, nil
	case KindDesign:
		return buildDesign(base, reference)
	default:
		return "", fmt.Errorf("unsupported resource kind %q", reference.Kind)
	}
}

func buildDesign(base string, reference Reference) (string, error) {
	switch reference.DesignKind {
	case DesignChapter, DesignSection, DesignFragment:
		if reference.ParentID != "" {
			return "", fmt.Errorf("%s design reference cannot have a parent", reference.DesignKind)
		}
		return base + string(reference.DesignKind) + ":" + reference.ID, nil
	case DesignLandmark:
		if err := validateID("fragment", reference.ParentID); err != nil {
			return "", err
		}
		return base + "fragment:" + reference.ParentID + ":landmark:" + reference.ID, nil
	default:
		return "", fmt.Errorf("unsupported design kind %q", reference.DesignKind)
	}
}

// Parse returns the structured form of a canonical living-Saga resource URN.
func Parse(value string) (Reference, error) {
	if !strings.HasPrefix(value, prefix) {
		return Reference{}, fmt.Errorf("URN must begin with %q", prefix)
	}
	parts := strings.Split(value, ":")
	var reference Reference
	switch len(parts) {
	case 5:
		reference = parseTopLevel(parts)
	case 7:
		reference = parseNested(parts)
	default:
		return Reference{}, fmt.Errorf("URN has an unsupported resource shape")
	}
	canonical, err := Build(reference)
	if err != nil {
		return Reference{}, fmt.Errorf("invalid living-Saga URN: %w", err)
	}
	if canonical != value {
		return Reference{}, fmt.Errorf("URN is not canonical; canonical form is %s", canonical)
	}
	return reference, nil
}

func parseTopLevel(parts []string) Reference {
	reference := Reference{SagaID: parts[2], ID: parts[4]}
	switch parts[3] {
	case string(KindStory):
		reference.Kind = KindStory
	case string(KindCitation):
		reference.Kind = KindCitation
	case string(KindRelation):
		reference.Kind = KindRelation
	case string(KindWave):
		reference.Kind = KindWave
	case string(KindWorkItem):
		reference.Kind = KindWorkItem
	case string(KindDependency):
		reference.Kind = KindDependency
	case string(KindContract):
		reference.Kind = KindContract
	case string(DesignChapter):
		reference.Kind, reference.DesignKind = KindDesign, DesignChapter
	case string(DesignSection):
		reference.Kind, reference.DesignKind = KindDesign, DesignSection
	case string(DesignFragment):
		reference.Kind, reference.DesignKind = KindDesign, DesignFragment
	}
	return reference
}

func parseNested(parts []string) Reference {
	reference := Reference{SagaID: parts[2], ParentID: parts[4], ID: parts[6]}
	switch {
	case parts[3] == string(KindStory) && parts[5] == string(KindCriterion):
		reference.Kind = KindCriterion
	case parts[3] == string(KindStory) && parts[5] == string(KindRevision):
		reference.Kind = KindRevision
	case parts[3] == string(DesignFragment) && parts[5] == string(DesignLandmark):
		reference.Kind, reference.DesignKind = KindDesign, DesignLandmark
	}
	return reference
}

func validateID(name, value string) error {
	if !ValidID(value) {
		return fmt.Errorf("%s ID must match %s", name, stableID.String())
	}
	return nil
}

// Story builds a stable story URN.
func Story(sagaID, storyID string) (string, error) {
	return Build(Reference{SagaID: sagaID, Kind: KindStory, ID: storyID})
}

// Criterion builds a stable criterion URN within a story.
func Criterion(sagaID, storyID, criterionID string) (string, error) {
	return Build(Reference{SagaID: sagaID, Kind: KindCriterion, ParentID: storyID, ID: criterionID})
}

// Revision builds a stable revision URN within a story.
func Revision(sagaID, storyID, revisionID string) (string, error) {
	return Build(Reference{SagaID: sagaID, Kind: KindRevision, ParentID: storyID, ID: revisionID})
}

// Citation builds a stable citation URN.
func Citation(sagaID, citationID string) (string, error) {
	return Build(Reference{SagaID: sagaID, Kind: KindCitation, ID: citationID})
}

// Relation builds a stable relation URN.
func Relation(sagaID, relationID string) (string, error) {
	return Build(Reference{SagaID: sagaID, Kind: KindRelation, ID: relationID})
}

// Design builds a stable chapter, section, or fragment design target. Use
// Landmark for a landmark nested beneath a fragment.
func Design(sagaID string, kind DesignKind, designID string) (string, error) {
	return Build(Reference{SagaID: sagaID, Kind: KindDesign, ID: designID, DesignKind: kind})
}

// Landmark builds a stable landmark design target within a fragment.
func Landmark(sagaID, fragmentID, landmarkID string) (string, error) {
	return Build(Reference{SagaID: sagaID, Kind: KindDesign, ID: landmarkID, ParentID: fragmentID, DesignKind: DesignLandmark})
}

// Wave builds a stable wave URN.
func Wave(sagaID, waveID string) (string, error) {
	return Build(Reference{SagaID: sagaID, Kind: KindWave, ID: waveID})
}

// WorkItem builds a stable work-item URN.
func WorkItem(sagaID, itemID string) (string, error) {
	return Build(Reference{SagaID: sagaID, Kind: KindWorkItem, ID: itemID})
}

// Dependency builds a stable dependency URN.
func Dependency(sagaID, dependencyID string) (string, error) {
	return Build(Reference{SagaID: sagaID, Kind: KindDependency, ID: dependencyID})
}

// Contract builds a stable contract URN.
func Contract(sagaID, contractID string) (string, error) {
	return Build(Reference{SagaID: sagaID, Kind: KindContract, ID: contractID})
}
