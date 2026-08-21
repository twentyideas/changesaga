package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type fakeQuerySession struct {
	snapshot string
	called   string
	request  any
	err      error
}

func (s *fakeQuerySession) Snapshot() string { return s.snapshot }

func (s *fakeQuerySession) Overview(_ context.Context, request overviewQuery) (any, error) {
	s.called, s.request = "overview", request
	return map[string]any{"operation": s.called}, s.err
}

func (s *fakeQuerySession) Children(_ context.Context, request childrenQuery) (queryPage, error) {
	s.called, s.request = "children", request
	return fakePage(s.called), s.err
}

func (s *fakeQuerySession) ReadFragment(_ context.Context, request fragmentQuery) (any, error) {
	s.called, s.request = "fragment", request
	return map[string]any{"operation": s.called}, s.err
}

func (s *fakeQuerySession) FragmentDiffs(_ context.Context, request fragmentDiffQuery) (queryPage, error) {
	s.called, s.request = "fragment-diffs", request
	return fakePage(s.called), s.err
}

func (s *fakeQuerySession) DiffOwners(_ context.Context, request diffOwnerQuery) (queryPage, error) {
	s.called, s.request = "diff-owners", request
	return fakePage(s.called), s.err
}

func (s *fakeQuerySession) Reviews(_ context.Context, request reviewQuery) (queryPage, error) {
	s.called, s.request = "reviews", request
	return fakePage(s.called), s.err
}

func (s *fakeQuerySession) Gaps(_ context.Context, request gapQuery) (queryPage, error) {
	s.called, s.request = "gaps", request
	return fakePage(s.called), s.err
}

func fakePage(operation string) queryPage {
	next := "next-page"
	return queryPage{Data: map[string]any{"operation": operation}, NextCursor: &next}
}

func TestQueryGoldenEnvelopes(t *testing.T) {
	tests := []struct {
		name string
		args []string
		open querySessionOpener
		exit int
	}{
		{
			name: "help",
			args: []string{"--help"},
			open: failIfOpened(t),
		},
		{
			name: "overview",
			args: []string{"overview", "--saga", "review.saga", "--repo", "source"},
			open: openFake(&fakeQuerySession{snapshot: "sha256:test"}),
		},
		{
			name: "children-page",
			args: []string{"children", "--saga", "review.saga", "--parent", "urn:change-saga:test:saga:test"},
			open: openFake(&fakeQuerySession{snapshot: "sha256:test"}),
		},
		{
			name: "invalid-request",
			args: []string{"fragment", "--target", "urn:change-saga:test:fragment:intro"},
			open: failIfOpened(t),
			exit: 2,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var out bytes.Buffer
			err := queryWithOpener(context.Background(), test.args, &out, test.open)
			assertStatus(t, err, test.exit)
			assertOneJSONValue(t, out.Bytes())
			want, readErr := os.ReadFile(filepath.Join("testdata", "query", test.name+".golden"))
			if readErr != nil {
				t.Fatal(readErr)
			}
			// Git may check text fixtures out with CRLF on Windows. The query
			// protocol itself deliberately emits canonical LF-delimited JSON.
			want = bytes.ReplaceAll(want, []byte("\r\n"), []byte("\n"))
			if !bytes.Equal(out.Bytes(), want) {
				t.Fatalf("envelope mismatch\n got: %s\nwant: %s", out.Bytes(), want)
			}
		})
	}
}

func TestQueryDispatchesEveryOperationAndPreservesArguments(t *testing.T) {
	tests := []struct {
		operation string
		args      []string
		want      any
	}{
		{"overview", nil, overviewQuery{}},
		{"children", []string{"--parent", "urn:parent", "--cursor", "c1", "--limit", "17"}, childrenQuery{Parent: "urn:parent", Cursor: "c1", Limit: 17}},
		{"fragment", []string{"--target", "urn:fragment", "--offset", "23", "--limit", "4096"}, fragmentQuery{Target: "urn:fragment", Offset: int64(23), Limit: 4096}},
		{"fragment-diffs", []string{"--target", "urn:fragment", "--cursor", "c2", "--limit", "18"}, fragmentDiffQuery{Target: "urn:fragment", Cursor: "c2", Limit: 18}},
		{"diff-owners", []string{"--diff", "saga-diff://v1/file?x", "--cursor", "c3", "--limit", "19"}, diffOwnerQuery{Diff: "saga-diff://v1/file?x", Cursor: "c3", Limit: 19}},
		{"reviews", []string{"--target", "urn:target", "--thread", "thread-1", "--state", "open", "--cursor", "c4", "--limit", "20"}, reviewQuery{Target: "urn:target", Thread: "thread-1", State: "open", Cursor: "c4", Limit: 20}},
		{"gaps", []string{"--kind", "stale", "--cursor", "c5", "--limit", "21"}, gapQuery{Kind: "stale", Cursor: "c5", Limit: 21}},
	}

	for _, test := range tests {
		t.Run(test.operation, func(t *testing.T) {
			session := &fakeQuerySession{snapshot: "sha256:snapshot"}
			args := append([]string{test.operation, "--saga", "review.saga", "--repo", "source-repo"}, test.args...)
			var opened queryOpenOptions
			open := func(_ context.Context, options queryOpenOptions) (querySession, error) {
				opened = options
				return session, nil
			}
			var out bytes.Buffer
			if err := queryWithOpener(context.Background(), args, &out, open); err != nil {
				t.Fatal(err)
			}
			if opened != (queryOpenOptions{SagaRoot: "review.saga", SourceDir: "source-repo"}) {
				t.Fatalf("open options = %#v", opened)
			}
			if session.called != test.operation || !reflect.DeepEqual(session.request, test.want) {
				t.Fatalf("dispatch = %q %#v, want %q %#v", session.called, session.request, test.operation, test.want)
			}
			assertOneJSONValue(t, out.Bytes())
		})
	}
}

func TestQueryMapsStableErrorsToStableExits(t *testing.T) {
	tests := []struct {
		code      string
		exit      int
		retryable bool
	}{
		{"invalid_argument", 2, false},
		{"invalid_saga", 3, false},
		{"stale_snapshot", 4, true},
		{"conflict", 4, false},
		{"not_found", 5, false},
		{"unsafe_path", 6, false},
		{"unsupported_media", 6, false},
		{"too_large", 6, false},
		{"source_unavailable", 7, true},
		{"internal", 1, false},
	}
	for _, test := range tests {
		t.Run(test.code, func(t *testing.T) {
			queryErr := &queryError{
				Code: test.code, Message: "stable message", Retryable: test.retryable,
				Details: map[string]any{"field": "value"},
			}
			var out bytes.Buffer
			err := queryWithOpener(context.Background(), []string{"overview", "--saga", "review.saga"}, &out,
				func(context.Context, queryOpenOptions) (querySession, error) { return nil, queryErr })
			assertStatus(t, err, test.exit)
			var envelope queryEnvelope
			decodeOneJSONValue(t, out.Bytes(), &envelope)
			if envelope.OK || envelope.Error == nil || envelope.Error.Code != test.code || envelope.Error.Retryable != test.retryable {
				t.Fatalf("wrong error envelope: %#v", envelope)
			}
		})
	}
}

func TestQueryRejectsAdversarialArgumentsBeforeOpening(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"unknown operation", []string{"overview\n{\"ok\":true}", "--saga", "x"}},
		{"unknown flag", []string{"overview", "--saga", "x", "--output", "/tmp/leak"}},
		{"positional injection", []string{"overview", "--saga", "x", "{\"ok\":true}"}},
		{"missing saga", []string{"overview"}},
		{"empty saga", []string{"overview", "--saga", " \t"}},
		{"missing parent", []string{"children", "--saga", "x"}},
		{"missing target", []string{"fragment", "--saga", "x"}},
		{"missing diff", []string{"diff-owners", "--saga", "x"}},
		{"negative offset", []string{"fragment", "--saga", "x", "--target", "urn:x", "--offset", "-1"}},
		{"zero limit", []string{"gaps", "--saga", "x", "--limit", "0"}},
		{"huge limit", []string{"gaps", "--saga", "x", "--limit", "1001"}},
		{"huge fragment chunk", []string{"fragment", "--saga", "x", "--target", "urn:x", "--limit", "1048577"}},
		{"nonnumeric limit", []string{"gaps", "--saga", "x", "--limit", "NaN"}},
		{"bad gap kind", []string{"gaps", "--saga", "x", "--kind", "orphan"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var out bytes.Buffer
			err := queryWithOpener(context.Background(), test.args, &out, failIfOpened(t))
			assertStatus(t, err, 2)
			var envelope queryEnvelope
			decodeOneJSONValue(t, out.Bytes(), &envelope)
			if envelope.OK || envelope.Error == nil || envelope.Error.Code != "invalid_argument" {
				t.Fatalf("wrong invalid request envelope: %#v", envelope)
			}
		})
	}
}

func TestQueryHelpNeverOpensSessionAndAlwaysUsesOneEnvelope(t *testing.T) {
	for _, args := range [][]string{nil, {"help"}, {"-h"}, {"--help"}, {"reviews", "--help"}} {
		var out bytes.Buffer
		if err := queryWithOpener(context.Background(), args, &out, failIfOpened(t)); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		var envelope queryEnvelope
		decodeOneJSONValue(t, out.Bytes(), &envelope)
		if !envelope.OK || envelope.Data == nil {
			t.Fatalf("%v: wrong help envelope: %#v", args, envelope)
		}
	}
}

func TestQueryRedactsUnexpectedApplicationErrors(t *testing.T) {
	secret := "/absolute/private/path/review.saga"
	session := &fakeQuerySession{snapshot: "sha256:test", err: errors.New("failed reading " + secret)}
	var out bytes.Buffer
	err := queryWithOpener(context.Background(), []string{"overview", "--saga", "review.saga"}, &out, openFake(session))
	assertStatus(t, err, 1)
	if strings.Contains(out.String(), secret) {
		t.Fatalf("unexpected error leaked a path: %s", out.String())
	}
	var envelope queryEnvelope
	decodeOneJSONValue(t, out.Bytes(), &envelope)
	if envelope.Error == nil || envelope.Error.Code != "internal" || envelope.Error.Message != "an unexpected error occurred" {
		t.Fatalf("unexpected error was not normalized: %#v", envelope)
	}
}

func TestQueryUnknownOperationDetailsDoNotEchoPathShapedInput(t *testing.T) {
	var out bytes.Buffer
	err := queryWithOpener(context.Background(), []string{"/private/path"}, &out, failIfOpened(t))
	assertStatus(t, err, 2)
	var envelope queryEnvelope
	decodeOneJSONValue(t, out.Bytes(), &envelope)
	details, ok := envelope.Error.Details.(map[string]any)
	if !ok {
		t.Fatalf("details = %#v", envelope.Error.Details)
	}
	if _, leaked := details["operation"]; leaked {
		t.Fatalf("raw operation leaked through details: %#v", details)
	}
	if strings.Contains(envelope.Error.Message, "/private/path") {
		t.Fatalf("raw operation leaked through message: %#v", envelope.Error)
	}
}

func openFake(session querySession) querySessionOpener {
	return func(context.Context, queryOpenOptions) (querySession, error) { return session, nil }
}

func failIfOpened(t *testing.T) querySessionOpener {
	t.Helper()
	return func(context.Context, queryOpenOptions) (querySession, error) {
		t.Fatal("query session opened for a request that should be handled by the adapter")
		return nil, errors.New("unreachable")
	}
}

func assertStatus(t *testing.T, err error, want int) {
	t.Helper()
	if want == 0 {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		return
	}
	var status *StatusError
	if !errors.As(err, &status) || status.Code != want {
		t.Fatalf("status error = %#v, want exit %d", err, want)
	}
}

func assertOneJSONValue(t *testing.T, body []byte) {
	t.Helper()
	var value any
	decodeOneJSONValue(t, body, &value)
}

func decodeOneJSONValue(t *testing.T, body []byte, target any) {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(body))
	if err := decoder.Decode(target); err != nil {
		t.Fatalf("decode envelope: %v\n%s", err, body)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		t.Fatalf("stdout contained more than one JSON value: err=%v extra=%#v body=%s", err, extra, body)
	}
}
