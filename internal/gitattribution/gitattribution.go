package gitattribution

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	Committed   = "committed"
	Uncommitted = "uncommitted"
	Rewritten   = "rewritten"
	Unavailable = "history_unavailable"
)

type Attribution struct {
	State       string
	Name        string
	Email       string
	CommitID    string
	CommittedAt time.Time
}

type Resolver struct {
	root  string
	err   error
	mu    sync.Mutex
	cache map[string]Attribution
}

func New(ctx context.Context, fromDir string) *Resolver {
	output, err := exec.CommandContext(ctx, "git", "-C", fromDir, "rev-parse", "--show-toplevel").Output()
	root := strings.TrimSpace(string(output))
	if resolved, resolveErr := filepath.EvalSymlinks(root); resolveErr == nil {
		root = resolved
	}
	return &Resolver{root: root, err: err, cache: map[string]Attribution{}}
}

func (r *Resolver) Resolve(ctx context.Context, path string) Attribution {
	r.mu.Lock()
	if value, ok := r.cache[path]; ok {
		r.mu.Unlock()
		return value
	}
	r.mu.Unlock()

	value := r.resolve(ctx, path)
	r.mu.Lock()
	r.cache[path] = value
	r.mu.Unlock()
	return value
}

func (r *Resolver) resolve(ctx context.Context, path string) Attribution {
	if r.err != nil || r.root == "" {
		return Attribution{State: Unavailable}
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return Attribution{State: Unavailable}
	}
	if resolved, resolveErr := filepath.EvalSymlinks(abs); resolveErr == nil {
		abs = resolved
	}
	rel, err := filepath.Rel(r.root, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return Attribution{State: Unavailable}
	}
	rel = filepath.ToSlash(rel)
	status, statusErr := exec.CommandContext(ctx, "git", "-C", r.root, "status", "--porcelain", "--untracked-files=normal", "--", rel).Output()
	if statusErr == nil {
		line := strings.TrimSpace(string(status))
		if strings.HasPrefix(line, "??") || len(line) >= 1 && line[0] == 'A' {
			return Attribution{State: Uncommitted}
		}
	}
	if err := exec.CommandContext(ctx, "git", "-C", r.root, "ls-files", "--error-unmatch", "--", rel).Run(); err != nil {
		if _, statErr := os.Stat(abs); statErr == nil {
			return Attribution{State: Uncommitted}
		}
		return Attribution{State: Unavailable}
	}
	output, err := exec.CommandContext(ctx, "git", "-C", r.root, "log", "-1", "--follow", "--diff-filter=A", "--format=%H%x00%cN%x00%cE%x00%cI", "--", rel).Output()
	if err != nil {
		return Attribution{State: Unavailable}
	}
	parts := strings.Split(strings.TrimSpace(string(output)), "\x00")
	if len(parts) != 4 || parts[0] == "" {
		return Attribution{State: Rewritten}
	}
	committedAt, err := time.Parse(time.RFC3339, parts[3])
	if err != nil {
		return Attribution{State: Unavailable}
	}
	return Attribution{State: Committed, CommitID: parts[0], Name: parts[1], Email: parts[2], CommittedAt: committedAt}
}
