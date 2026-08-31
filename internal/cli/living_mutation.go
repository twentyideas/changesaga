package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"
)

// livingMutationOutput is deliberately small and shared by all living-Saga
// writers. Domain packages decide what was created; the CLI only adds the
// operation name and transport status.
type livingMutationOutput struct {
	OK        bool     `json:"ok"`
	Operation string   `json:"operation"`
	Resource  string   `json:"resource,omitempty"`
	Path      string   `json:"path,omitempty"`
	Created   []string `json:"created"`
	EventIDs  []string `json:"event_ids"`
	Replayed  bool     `json:"replayed"`
	Error     *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func writeLivingMutation(out io.Writer, operation, resource, path string, created, eventIDs []string, replayed, jsonOutput bool) error {
	if created == nil {
		created = []string{}
	}
	if eventIDs == nil {
		eventIDs = []string{}
	}
	if jsonOutput {
		return writeJSON(out, livingMutationOutput{
			OK: true, Operation: operation, Resource: resource, Path: path,
			Created: created, EventIDs: eventIDs, Replayed: replayed,
		})
	}
	verb := "Created"
	if replayed {
		verb = "Replayed"
	}
	fmt.Fprintf(out, "%s %s\n", verb, resource)
	if path != "" {
		fmt.Fprintf(out, "Path: %s\n", path)
	}
	return nil
}

func reportLivingMutationFailure(out io.Writer, operation string, err error) error {
	if err == nil || err == flag.ErrHelp {
		return err
	}
	result := livingMutationOutput{
		OK: false, Operation: operation, Created: []string{}, EventIDs: []string{},
	}
	result.Error = &struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}{Code: "mutation_failed", Message: err.Error()}
	if writeErr := writeJSON(out, result); writeErr != nil {
		return writeErr
	}
	return &StatusError{Code: 1}
}

func livingFamilyHelp(name string, operations []string, out io.Writer) error {
	flags := commandFlags(name, commandUsage[name], out)
	flags.Usage = func() {
		fmt.Fprintf(out, "Usage:\n  %s\n", commandUsage[name])
		if description := commandDescription[name]; description != "" {
			fmt.Fprintf(out, "\n%s\n", description)
		}
		fmt.Fprint(out, "\nOperations:\n")
		for _, operation := range operations {
			fmt.Fprintf(out, "  %s\n", operation)
		}
	}
	return flags.Parse([]string{"-h"})
}

// The frozen contract writes the Saga before flags while older Change Saga
// commands conventionally put it last. Accept both without changing the
// deterministic flag package used by every command.
func normalizeLivingArgs(args []string) []string {
	if len(args) > 1 && !strings.HasPrefix(args[0], "-") {
		result := append([]string{}, args[1:]...)
		return append(result, args[0])
	}
	return args
}

func requireLivingArgs(flags *flag.FlagSet, required ...string) error {
	if flags.NArg() != 1 {
		return fmt.Errorf("usage: %s", commandUsage[flags.Name()])
	}
	for _, value := range required {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("usage: %s", commandUsage[flags.Name()])
		}
	}
	return nil
}

func parseCriteria(values []string) ([]criterionInput, error) {
	criteria := make([]criterionInput, 0, len(values))
	for index, value := range values {
		id, statement, ok := strings.Cut(value, "=")
		if !ok || strings.TrimSpace(id) == "" || strings.TrimSpace(statement) == "" {
			return nil, fmt.Errorf("--criterion %d must be ID=STATEMENT", index+1)
		}
		criteria = append(criteria, criterionInput{ID: strings.TrimSpace(id), Statement: strings.TrimSpace(statement)})
	}
	return criteria, nil
}

type criterionInput struct {
	ID        string
	Statement string
}

func decodeInlineJSON(raw, name string, target any) error {
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("invalid %s JSON: %w", name, err)
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return fmt.Errorf("invalid %s JSON: trailing content", name)
	}
	return nil
}
