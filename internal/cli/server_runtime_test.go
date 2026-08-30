package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestDetachedServerActiveRequiresCompleteReviewPage(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/runtime", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		<-r.Context().Done()
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	state := detachedServerState{URL: server.URL}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if detachedServerActive(ctx, state) {
		t.Fatal("server with a spinning review page was reported active")
	}
	if !detachedServerReachable(context.Background(), state) {
		t.Fatal("spinning review page made the independent control plane unreachable")
	}
}

func TestStopIncludesUnhealthyControllableServer(t *testing.T) {
	root := newAuthoredSaga(t)
	t.Setenv("CHANGE_SAGA_RUNTIME_DIR", t.TempDir())
	statePath, err := detachedStatePath(root)
	if err != nil {
		t.Fatal(err)
	}

	var server *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/runtime", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	mux.HandleFunc("POST /api/runtime-stop", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Change-Saga-Shutdown"); got != "test-shutdown-token" {
			http.Error(w, "bad token", http.StatusForbidden)
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
		go func() {
			time.Sleep(10 * time.Millisecond)
			server.Close()
		}()
	})
	mux.HandleFunc("GET /", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "review page unavailable", http.StatusServiceUnavailable)
	})
	server = httptest.NewServer(mux)
	defer server.Close()

	state := detachedServerState{Saga: root, PID: 4242, URL: server.URL, ShutdownToken: "test-shutdown-token"}
	if err := writeDetachedState(statePath, state); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := manageDetachedServers(context.Background(), "stop", []string{root}, &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "stopped  PID 4242") {
		t.Fatalf("stop output = %q", output.String())
	}
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Fatalf("unhealthy server state was not removed: %v", err)
	}
}

func TestManagedServerStatusAndStopLifecycle(t *testing.T) {
	root := newAuthoredSaga(t)
	t.Setenv("CHANGE_SAGA_RUNTIME_DIR", t.TempDir())
	statePath, err := detachedStatePath(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- runManagedServer(ctx, root, "", "127.0.0.1:0", false, statePath, "test-shutdown-token", &bytes.Buffer{})
	}()

	var state detachedServerState
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		state, err = readDetachedState(statePath)
		if err == nil && detachedServerActive(context.Background(), state) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil || !detachedServerActive(context.Background(), state) {
		t.Fatalf("managed server did not publish active state: %#v, %v", state, err)
	}
	info, statErr := os.Stat(statePath)
	if statErr != nil || runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("runtime state must be private: info=%v err=%v", info, statErr)
	}

	var status bytes.Buffer
	if err := manageDetachedServers(context.Background(), "status", []string{"--json", root}, &status); err != nil {
		t.Fatal(err)
	}
	var states []detachedServerPublicState
	if err := json.Unmarshal(status.Bytes(), &states); err != nil || len(states) != 1 || !states[0].Active || states[0].PID != os.Getpid() {
		t.Fatalf("status = %#v, err=%v\n%s", states, err, status.String())
	}
	if bytes.Contains(status.Bytes(), []byte("shutdown_token")) || bytes.Contains(status.Bytes(), []byte("test-shutdown-token")) {
		t.Fatalf("public status exposed its shutdown secret:\n%s", status.String())
	}

	var stopped bytes.Buffer
	if err := manageDetachedServers(context.Background(), "stop", []string{root}, &stopped); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-serverDone:
		if err != nil {
			t.Fatalf("managed server exit: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("managed server did not stop")
	}
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Fatalf("managed server left runtime state behind: %v", err)
	}
}
