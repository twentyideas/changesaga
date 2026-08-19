// Package gitattribution derives review identity from the Git commit that
// introduced an event file. Event payloads are deliberately not consulted.
package gitattribution

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type Status string

const (
	Committed          Status = "committed"
	Uncommitted        Status = "uncommitted"
	HistoryUnavailable Status = "history_unavailable"
)

type Committer struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

type Attribution struct {
	Status      Status     `json:"status"`
	Commit      string     `json:"commit,omitempty"`
	Committer   *Committer `json:"committer,omitempty"`
	CommittedAt *time.Time `json:"committed_at,omitempty"`
}

// Resolve resolves all event paths at a load boundary. The result is keyed by
// the cleaned absolute path supplied by the caller. History is intentionally
// queried afresh so an amended or rebased introduction yields its current
// committer rather than a stale cached identity.
func Resolve(ctx context.Context, sagaRoot string, eventPaths []string) map[string]Attribution {
	result := make(map[string]Attribution, len(eventPaths))
	for _, path := range eventPaths {
		set(result, path, Attribution{Status: HistoryUnavailable})
	}
	if len(eventPaths) == 0 {
		return result
	}

	worktreeOutput, err := git(ctx, sagaRoot, "rev-parse", "--show-toplevel")
	if err != nil {
		return result
	}
	worktree := clean(strings.TrimSpace(worktreeOutput))
	sagaRoot = clean(sagaRoot)
	if !within(worktree, sagaRoot) {
		return result
	}

	for _, suppliedPath := range eventPaths {
		path := clean(suppliedPath)
		if !within(sagaRoot, path) || !within(worktree, path) {
			continue
		}
		rel, err := filepath.Rel(worktree, path)
		if err != nil {
			continue
		}
		rel = filepath.ToSlash(rel)

		// A file absent from HEAD is local, whether untracked or staged-new.
		if _, err := git(ctx, worktree, "cat-file", "-e", "HEAD:"+rel); err != nil {
			set(result, suppliedPath, Attribution{Status: Uncommitted})
			continue
		}

		output, err := git(ctx, worktree, "log", "--follow", "--diff-filter=A", "--format=%H%x00%cn%x00%ce%x00%cI%x00", "--", rel)
		if err != nil {
			continue
		}
		fields := nonEmptyNULFields(output)
		if len(fields) < 4 {
			continue
		}
		// git log is newest-first. The final addition is the original
		// introduction, including across a chain of renames followed by --follow.
		start := len(fields) - 4
		committedAt, err := time.Parse(time.RFC3339, fields[start+3])
		if err != nil {
			continue
		}
		set(result, suppliedPath, Attribution{
			Status: Committed, Commit: fields[start],
			Committer:   &Committer{Name: fields[start+1], Email: fields[start+2]},
			CommittedAt: &committedAt,
		})
	}
	return result
}

func git(ctx context.Context, dir string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	output, err := command.Output()
	return string(output), err
}

func nonEmptyNULFields(output string) []string {
	raw := strings.Split(output, "\x00")
	fields := make([]string, 0, len(raw))
	for _, value := range raw {
		value = strings.TrimSpace(value)
		if value != "" {
			fields = append(fields, value)
		}
	}
	return fields
}

func clean(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		abs = real
	}
	return filepath.Clean(abs)
}

func set(result map[string]Attribution, path string, attribution Attribution) {
	abs, err := filepath.Abs(path)
	if err == nil {
		result[filepath.Clean(abs)] = attribution
	}
	result[clean(path)] = attribution
}

func within(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
