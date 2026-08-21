package gitattribution

import (
	"bytes"
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
	root       string
	err        error
	command    func(context.Context, ...string) *exec.Cmd
	indexOnce  sync.Once
	statusErr  error
	trackedErr error
	added      map[string]bool
	tracked    map[string]bool
	mu         sync.Mutex
	cache      map[string]Attribution
}

func New(ctx context.Context, fromDir string) *Resolver {
	output, err := exec.CommandContext(ctx, "git", "-C", fromDir, "rev-parse", "--show-toplevel").Output()
	root := strings.TrimSpace(string(output))
	if resolved, resolveErr := filepath.EvalSymlinks(root); resolveErr == nil {
		root = resolved
	}
	return &Resolver{root: root, err: err, command: gitCommand, cache: map[string]Attribution{}}
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
	r.loadIndex(ctx)
	if r.statusErr == nil {
		if r.added[rel] {
			return Attribution{State: Uncommitted}
		}
	} else if isAdded(ctx, r.command, r.root, rel) {
		return Attribution{State: Uncommitted}
	}
	tracked := r.tracked[rel]
	if r.trackedErr != nil {
		tracked = r.command(ctx, "-C", r.root, "ls-files", "--error-unmatch", "--", rel).Run() == nil
	}
	if !tracked {
		if _, statErr := os.Stat(abs); statErr == nil {
			return Attribution{State: Uncommitted}
		}
		return Attribution{State: Unavailable}
	}
	output, err := r.command(ctx, "-C", r.root, "log", "-1", "--follow", "--diff-filter=A", "--format=%H%x00%cN%x00%cE%x00%cI", "--", rel).Output()
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

// loadIndex snapshots the two repository-wide facts shared by every path
// attribution. A large saga can reference hundreds of review records; loading
// Git's index once avoids two subprocesses and two index reads per record.
func (r *Resolver) loadIndex(ctx context.Context) {
	r.indexOnce.Do(func() {
		r.added = map[string]bool{}
		status, err := r.command(ctx, "-C", r.root, "status", "--porcelain=v1", "-z", "--untracked-files=no").Output()
		r.statusErr = err
		if err == nil {
			parseAdded(status, r.added)
		}

		r.tracked = map[string]bool{}
		tracked, err := r.command(ctx, "-C", r.root, "ls-files", "-z").Output()
		r.trackedErr = err
		if err == nil {
			for _, path := range bytes.Split(tracked, []byte{0}) {
				if len(path) > 0 {
					r.tracked[string(path)] = true
				}
			}
		}
	})
}

func parseAdded(status []byte, added map[string]bool) {
	records := bytes.Split(status, []byte{0})
	for i := 0; i < len(records); i++ {
		record := records[i]
		if len(record) < 4 || record[2] != ' ' {
			continue
		}
		if record[0] == 'A' {
			added[string(record[3:])] = true
		}
		if record[0] == 'R' || record[0] == 'C' || record[1] == 'R' || record[1] == 'C' {
			i++ // Porcelain -z follows a rename/copy destination with its source.
		}
	}
}

func isAdded(ctx context.Context, command func(context.Context, ...string) *exec.Cmd, root, rel string) bool {
	status, err := command(ctx, "-C", root, "status", "--porcelain=v1", "-z", "--untracked-files=no", "--", rel).Output()
	if err != nil {
		return false
	}
	added := map[string]bool{}
	parseAdded(status, added)
	return added[rel]
}

func gitCommand(ctx context.Context, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, "git", args...)
}
