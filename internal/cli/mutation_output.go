package cli

import (
	"flag"
	"io"
	"strings"
)

type mutationFailure struct {
	Message string `json:"message"`
}

type mutationFailureOutput struct {
	OK            bool              `json:"ok"`
	Records       int               `json:"records"`
	Selectors     int               `json:"selectors"`
	EvidenceFiles []string          `json:"evidence_files"`
	Failures      []mutationFailure `json:"failures"`
}

func jsonFlagRequested(args []string) bool {
	for _, arg := range args {
		if arg == "--json" || arg == "-json" || strings.HasPrefix(arg, "--json=") || strings.HasPrefix(arg, "-json=") {
			return !strings.HasSuffix(arg, "=false")
		}
	}
	return false
}

func reportJSONMutationFailure(out io.Writer, err error) error {
	if err == nil || err == flag.ErrHelp {
		return err
	}
	if writeErr := writeJSON(out, mutationFailureOutput{OK: false, EvidenceFiles: []string{}, Failures: []mutationFailure{{Message: err.Error()}}}); writeErr != nil {
		return writeErr
	}
	return &StatusError{Code: 1}
}
