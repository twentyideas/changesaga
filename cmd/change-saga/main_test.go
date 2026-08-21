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
