package cli

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	reviewserver "github.com/twentyideas/changesaga/internal/server"
	"github.com/twentyideas/changesaga/internal/store"
)

const (
	runtimeStateEnv = "CHANGE_SAGA_RUNTIME_STATE"
	runtimeTokenEnv = "CHANGE_SAGA_RUNTIME_TOKEN"
)

type detachedServerState struct {
	Saga          string    `json:"saga"`
	Source        string    `json:"source,omitempty"`
	PID           int       `json:"pid"`
	URL           string    `json:"url"`
	StartedAt     time.Time `json:"started_at"`
	ShutdownToken string    `json:"shutdown_token"`
	Log           string    `json:"log"`
	Active        bool      `json:"active,omitempty"`
}

type detachedServerPublicState struct {
	Saga      string    `json:"saga"`
	Source    string    `json:"source,omitempty"`
	PID       int       `json:"pid"`
	URL       string    `json:"url"`
	StartedAt time.Time `json:"started_at"`
	Log       string    `json:"log"`
	Active    bool      `json:"active"`
}

func startDetachedServer(ctx context.Context, root, sourceDir, addr string, openBrowser bool, out io.Writer) error {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	statePath, err := detachedStatePath(absRoot)
	if err != nil {
		return err
	}
	if existing, readErr := readDetachedState(statePath); readErr == nil && detachedServerActive(ctx, existing) {
		if openBrowser {
			_ = reviewserver.OpenBrowser(existing.URL)
		}
		fmt.Fprintf(out, "Change Saga is already running at %s (PID %d)\n", existing.URL, existing.PID)
		return nil
	}
	_ = os.Remove(statePath)
	token, err := runtimeToken()
	if err != nil {
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate change-saga executable: %w", err)
	}
	args := []string{"serve", "--addr", addr}
	if sourceDir != "" {
		absSource, absErr := filepath.Abs(sourceDir)
		if absErr != nil {
			return absErr
		}
		sourceDir = absSource
		args = append(args, "--repo", sourceDir)
	}
	if openBrowser {
		args = append(args, "--open")
	}
	args = append(args, absRoot)
	logPath := strings.TrimSuffix(statePath, ".json") + ".log"
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("open detached server log: %w", err)
	}
	command := exec.Command(executable, args...)
	command.Env = append(os.Environ(), runtimeStateEnv+"="+statePath, runtimeTokenEnv+"="+token)
	command.Stdout, command.Stderr = logFile, logFile
	configureDetachedProcess(command)
	if err := command.Start(); err != nil {
		_ = logFile.Close()
		return fmt.Errorf("start detached server: %w", err)
	}
	_ = logFile.Close()
	pid := command.Process.Pid
	_ = command.Process.Release()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		state, readErr := readDetachedState(statePath)
		if readErr == nil && state.PID == pid && detachedServerActive(ctx, state) {
			fmt.Fprintf(out, "Change Saga is available at %s\nPID: %d\nLog: %s\n", state.URL, state.PID, state.Log)
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
	logTail, _ := os.ReadFile(logPath)
	if len(logTail) > 4096 {
		logTail = logTail[len(logTail)-4096:]
	}
	return fmt.Errorf("detached server did not become ready within 10s; log %s: %s", logPath, strings.TrimSpace(string(logTail)))
}

func runManagedServer(ctx context.Context, root, sourceDir, addr string, openBrowser bool, statePath, token string, out io.Writer) error {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	if sourceDir != "" {
		sourceDir, err = filepath.Abs(sourceDir)
		if err != nil {
			return err
		}
	}
	logPath := strings.TrimSuffix(statePath, ".json") + ".log"
	options := reviewserver.ManagedOptions{ShutdownToken: token, OnReady: func(url string) error {
		return writeDetachedState(statePath, detachedServerState{Saga: absRoot, Source: sourceDir, PID: os.Getpid(), URL: url, StartedAt: time.Now().UTC(), ShutdownToken: token, Log: logPath})
	}}
	defer removeDetachedState(statePath, os.Getpid())
	return reviewserver.ListenManaged(ctx, absRoot, sourceDir, addr, openBrowser, out, options)
}

func manageDetachedServers(ctx context.Context, operation string, args []string, out io.Writer) error {
	flags := commandFlags("serve "+operation, "change-saga serve "+operation+" [--json] [<saga>]", out)
	jsonOutput := flags.Bool("json", false, "emit machine-readable runtime state")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() > 1 {
		return fmt.Errorf("usage: change-saga serve %s [--json] [<saga>]", operation)
	}
	states, err := detachedStates()
	if err != nil {
		return err
	}
	if flags.NArg() == 1 {
		wanted, absErr := filepath.Abs(flags.Arg(0))
		if absErr != nil {
			return absErr
		}
		filtered := states[:0]
		for _, state := range states {
			if state.Saga == wanted {
				filtered = append(filtered, state)
			}
		}
		states = filtered
	}
	for index := range states {
		states[index].Active = detachedServerActive(ctx, states[index])
	}
	if operation == "stop" {
		active := states[:0]
		for _, state := range states {
			if state.Active {
				active = append(active, state)
			}
		}
		states = active
		if len(states) == 0 {
			return fmt.Errorf("no matching detached Change Saga server is running")
		}
		if flags.NArg() == 0 && len(states) > 1 {
			return fmt.Errorf("more than one detached server is running; pass the saga path to stop one")
		}
		if err := stopDetachedServer(ctx, states[0]); err != nil {
			return err
		}
		states[0].Active = false
	}
	if *jsonOutput {
		public := make([]detachedServerPublicState, 0, len(states))
		for _, state := range states {
			public = append(public, detachedServerPublicState{Saga: state.Saga, Source: state.Source, PID: state.PID, URL: state.URL, StartedAt: state.StartedAt, Log: state.Log, Active: state.Active})
		}
		return writeJSON(out, public)
	}
	if len(states) == 0 {
		fmt.Fprintln(out, "No detached Change Saga servers are running.")
		return nil
	}
	for _, state := range states {
		status := "stale"
		if state.Active {
			status = "running"
		}
		if operation == "stop" {
			status = "stopped"
		}
		fmt.Fprintf(out, "%s  PID %d  %s  %s\n", status, state.PID, state.URL, state.Saga)
	}
	return nil
}

func detachedStatePath(absSaga string) (string, error) {
	dir, err := detachedRuntimeDir()
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256([]byte(absSaga))
	return filepath.Join(dir, hex.EncodeToString(digest[:16])+".json"), nil
}

func detachedRuntimeDir() (string, error) {
	if override := os.Getenv("CHANGE_SAGA_RUNTIME_DIR"); override != "" {
		if err := os.MkdirAll(override, 0o700); err != nil {
			return "", err
		}
		return override, nil
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(cache, "change-saga", "servers")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	_ = os.Chmod(dir, 0o700)
	return dir, nil
}

func runtimeToken() (string, error) {
	var value [32]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("create detached server token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(value[:]), nil
}

func writeDetachedState(path string, state detachedServerState) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return store.WriteFile(path, append(data, '\n'), 0o600, false)
}

func readDetachedState(path string) (detachedServerState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return detachedServerState{}, err
	}
	var state detachedServerState
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&state); err != nil {
		return detachedServerState{}, err
	}
	if state.PID <= 0 || state.URL == "" || state.Saga == "" || state.ShutdownToken == "" {
		return detachedServerState{}, errors.New("invalid detached server state")
	}
	return state, nil
}

func detachedStates() ([]detachedServerState, error) {
	dir, err := detachedRuntimeDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var states []detachedServerState
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		state, readErr := readDetachedState(path)
		if readErr != nil {
			continue
		}
		states = append(states, state)
	}
	sort.Slice(states, func(i, j int) bool { return states[i].Saga < states[j].Saga })
	return states, nil
}

func detachedServerActive(ctx context.Context, state detachedServerState) bool {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(state.URL, "/")+"/api/runtime", nil)
	if err != nil {
		return false
	}
	client := &http.Client{Timeout: 500 * time.Millisecond}
	response, err := client.Do(request)
	if err != nil {
		return false
	}
	defer response.Body.Close()
	return response.StatusCode == http.StatusOK
}

func stopDetachedServer(ctx context.Context, state detachedServerState) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(state.URL, "/")+"/api/runtime-stop", nil)
	if err != nil {
		return err
	}
	request.Header.Set("X-Change-Saga-Shutdown", state.ShutdownToken)
	client := &http.Client{Timeout: 2 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("stop detached server PID %d: %w", state.PID, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("stop detached server PID %d: HTTP %s", state.PID, response.Status)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !detachedServerActive(ctx, state) {
			path, _ := detachedStatePath(state.Saga)
			removeDetachedState(path, state.PID)
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("detached server PID %d did not stop within 5s", state.PID)
}

func removeDetachedState(path string, pid int) {
	state, err := readDetachedState(path)
	if err != nil || state.PID != pid {
		return
	}
	_ = os.Remove(path)
	_ = store.SyncDir(filepath.Dir(path))
}
