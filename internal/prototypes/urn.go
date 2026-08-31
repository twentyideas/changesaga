package prototypes

import (
	"fmt"
	"strings"

	"github.com/twentyideas/changesaga/internal/livingid"
)

func PrototypeURN(sagaID, prototypeID string) (string, error) {
	return buildURN(sagaID, "prototype", prototypeID)
}

func RevisionURN(sagaID, prototypeID, revisionID string) (string, error) {
	if _, err := PrototypeURN(sagaID, prototypeID); err != nil {
		return "", err
	}
	if !livingid.ValidID(revisionID) {
		return "", fmt.Errorf("revision ID is not a stable identifier")
	}
	return fmt.Sprintf("urn:change-saga:%s:prototype:%s:revision:%s", sagaID, prototypeID, revisionID), nil
}

func AnnotationURN(sagaID, prototypeID, annotationID string) (string, error) {
	if _, err := PrototypeURN(sagaID, prototypeID); err != nil {
		return "", err
	}
	if !livingid.ValidID(annotationID) {
		return "", fmt.Errorf("annotation ID is not a stable identifier")
	}
	return fmt.Sprintf("urn:change-saga:%s:prototype:%s:annotation:%s", sagaID, prototypeID, annotationID), nil
}

func buildURN(sagaID, kind, id string) (string, error) {
	if !livingid.ValidID(sagaID) {
		return "", fmt.Errorf("saga ID is not a stable identifier")
	}
	if !livingid.ValidID(id) {
		return "", fmt.Errorf("%s ID is not a stable identifier", kind)
	}
	return fmt.Sprintf("urn:change-saga:%s:%s:%s", sagaID, kind, id), nil
}

func parsePrototypeURN(value, sagaID string) (string, error) {
	parts := strings.Split(value, ":")
	if len(parts) != 5 || parts[0] != "urn" || parts[1] != "change-saga" || parts[2] != sagaID || parts[3] != "prototype" || !livingid.ValidID(parts[4]) {
		return "", fmt.Errorf("must be a canonical prototype URN in saga %q", sagaID)
	}
	want, _ := PrototypeURN(sagaID, parts[4])
	if want != value {
		return "", fmt.Errorf("prototype URN is not canonical")
	}
	return parts[4], nil
}

func parseRevisionURN(value, sagaID, prototypeID string) (string, error) {
	parts := strings.Split(value, ":")
	if len(parts) != 7 || parts[0] != "urn" || parts[1] != "change-saga" || parts[2] != sagaID || parts[3] != "prototype" || parts[4] != prototypeID || parts[5] != "revision" || !livingid.ValidID(parts[6]) {
		return "", fmt.Errorf("must be a canonical revision URN for prototype %q", prototypeID)
	}
	return parts[6], nil
}

func parseTargetURN(value, sagaID string) (storyID string, criterion bool, err error) {
	ref, err := livingid.Parse(value)
	if err != nil || ref.SagaID != sagaID || (ref.Kind != livingid.KindStory && ref.Kind != livingid.KindCriterion) {
		return "", false, fmt.Errorf("target must be a canonical story or criterion URN in saga %q", sagaID)
	}
	if ref.Kind == livingid.KindCriterion {
		return ref.ParentID, true, nil
	}
	return ref.ID, false, nil
}

func parseStoryRevision(value, sagaID, storyID string) error {
	ref, err := livingid.Parse(value)
	if err != nil || ref.SagaID != sagaID || ref.Kind != livingid.KindRevision || (ref.ParentKind != "" && ref.ParentKind != livingid.KindStory) || ref.ParentID != storyID {
		return fmt.Errorf("story_revision must pin target story %q", storyID)
	}
	return nil
}
