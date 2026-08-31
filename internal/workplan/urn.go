package workplan

import (
	"fmt"

	"github.com/twentyideas/changesaga/internal/livingid"
)

func ProgressEventURN(sagaID, itemID, eventID string) (string, error) {
	return itemEventURN(sagaID, itemID, "progress-event", eventID)
}

func WorkspaceEventURN(sagaID, itemID, eventID string) (string, error) {
	return itemEventURN(sagaID, itemID, "workspace-event", eventID)
}

func MergeEventURN(sagaID, itemID, eventID string) (string, error) {
	return itemEventURN(sagaID, itemID, "merge-event", eventID)
}

func ContractEventURN(sagaID, contractID, eventID string) (string, error) {
	for name, value := range map[string]string{"saga": sagaID, "contract": contractID, "event": eventID} {
		if !livingid.ValidID(value) {
			return "", fmt.Errorf("%s ID is not a stable identifier", name)
		}
	}
	return fmt.Sprintf("urn:change-saga:%s:contract:%s:event:%s", sagaID, contractID, eventID), nil
}

func itemEventURN(sagaID, itemID, kind, eventID string) (string, error) {
	for name, value := range map[string]string{"saga": sagaID, "work item": itemID, "event": eventID} {
		if !livingid.ValidID(value) {
			return "", fmt.Errorf("%s ID is not a stable identifier", name)
		}
	}
	return fmt.Sprintf("urn:change-saga:%s:work-item:%s:%s:%s", sagaID, itemID, kind, eventID), nil
}

func eventURNForWorkItem(workItem, kind, eventID string) string {
	return workItem + ":" + kind + ":" + eventID
}

func eventURNForContract(contract, eventID string) string {
	return contract + ":event:" + eventID
}
