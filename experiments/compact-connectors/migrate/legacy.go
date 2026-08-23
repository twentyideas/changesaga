package migrate

import (
	"encoding/json"
	"os"

	"github.com/twentyideas/changesaga/internal/saga"
)

// writeLegacyRecord emits one v2 evidence file. It uses the same field shape
// and indentation the reference CLI writes so a reverse-migrated saga is
// ordinary v2 data, not a dialect of it.
func writeLegacyRecord(path string, references []saga.DiffReference) (int64, error) {
	document := struct {
		Version int                  `json:"version"`
		Diffs   []saga.DiffReference `json:"diffs"`
	}{Version: saga.CurrentVersion, Diffs: references}

	encoded, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return 0, err
	}
	encoded = append(encoded, '\n')
	if err := os.WriteFile(path, encoded, 0o644); err != nil {
		return 0, err
	}
	return int64(len(encoded)), nil
}
