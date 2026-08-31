package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"testing"
)

func TestRunQueryKeepsMachineOutputOnStdout(t *testing.T) {
	tests := []struct {
		name string
		args []string
		exit int
	}{
		{"help", []string{"query", "--help"}, 0},
		{"invalid", []string{"query", "overview"}, 2},
		{"missing saga", []string{"query", "overview", "--saga", "review.saga"}, 5},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if got := run(test.args, &stdout, &stderr); got != test.exit {
				t.Fatalf("exit = %d, want %d", got, test.exit)
			}
			if stderr.Len() != 0 {
				t.Fatalf("structured query wrote stderr: %q", stderr.String())
			}
			decoder := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
			var envelope map[string]any
			if err := decoder.Decode(&envelope); err != nil {
				t.Fatalf("decode stdout: %v\n%s", err, stdout.String())
			}
			if err := decoder.Decode(&map[string]any{}); !errors.Is(err, io.EOF) {
				t.Fatalf("stdout was not exactly one JSON value: %v\n%s", err, stdout.String())
			}
			if envelope["schema"] != "change-saga.ai/v1" {
				t.Fatalf("wrong schema: %#v", envelope)
			}
		})
	}
}

func TestRunMutationJSONKeepsFailureOnStdout(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if got := run([]string{"cover", "--uri", "not-a-uri", "--json", "missing.saga"}, &stdout, &stderr); got != 1 {
		t.Fatalf("exit = %d, want 1", got)
	}
	if stderr.Len() != 0 {
		t.Fatalf("structured mutation wrote stderr: %q", stderr.String())
	}
	var result struct {
		OK       bool `json:"ok"`
		Failures []struct {
			Message string `json:"message"`
		} `json:"failures"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil || result.OK || len(result.Failures) != 1 {
		t.Fatalf("mutation failure = %#v, err=%v\n%s", result, err, stdout.String())
	}
}

func TestRunLivingMutationFamiliesUseTheSupportedJSONContract(t *testing.T) {
	for _, family := range []string{"story", "citation", "relation", "plan"} {
		t.Run(family, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if got := run([]string{family, "unknown", "--json"}, &stdout, &stderr); got != 1 {
				t.Fatalf("exit = %d, want 1", got)
			}
			if stderr.Len() != 0 {
				t.Fatalf("structured mutation wrote stderr: %q", stderr.String())
			}
			var result struct {
				OK        bool   `json:"ok"`
				Operation string `json:"operation"`
				Created   []any  `json:"created"`
				EventIDs  []any  `json:"event_ids"`
				Error     *struct {
					Code    string `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
			}
			decoder := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
			if err := decoder.Decode(&result); err != nil {
				t.Fatalf("decode stdout: %v\n%s", err, stdout.String())
			}
			if err := decoder.Decode(&map[string]any{}); !errors.Is(err, io.EOF) {
				t.Fatalf("stdout was not exactly one JSON value: %v\n%s", err, stdout.String())
			}
			if result.OK || result.Operation != family+" unknown" || result.Error == nil || result.Error.Code == "" || result.Created == nil || result.EventIDs == nil {
				t.Fatalf("unexpected mutation failure: %#v", result)
			}
		})
	}
}
