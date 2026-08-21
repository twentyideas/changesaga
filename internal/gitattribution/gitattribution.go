package gitattribution

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
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
	root    string
	err     error
	mu      sync.Mutex
	cache   map[string]Attribution
	command func(context.Context, ...string) ([]byte, error)
}

func New(ctx context.Context, fromDir string) *Resolver {
	output, err := exec.CommandContext(ctx, "git", "-C", fromDir, "rev-parse", "--show-toplevel").Output()
	root := strings.TrimSpace(string(output))
	if resolved, resolveErr := filepath.EvalSymlinks(root); resolveErr == nil {
		root = resolved
	}
	return &Resolver{root: root, err: err, cache: map[string]Attribution{}, command: execGitOutput}
}

func (r *Resolver) Resolve(ctx context.Context, path string) Attribution {
	return r.ResolveAll(ctx, []string{path})[0]
}

// ResolveAll resolves one page's review records together. Repository state and
// tracked-file membership are properties of the snapshot, so querying them once
// avoids two Git processes per record. Introducing commits remain per-path so
// --follow keeps its rename semantics.
func (r *Resolver) ResolveAll(ctx context.Context, paths []string) []Attribution {
	result := make([]Attribution, len(paths))
	pending := make([]bool, len(paths))
	r.mu.Lock()
	for index, path := range paths {
		if value, ok := r.cache[path]; ok {
			result[index] = value
		} else {
			pending[index] = true
		}
	}
	r.mu.Unlock()

	type pathGroup struct {
		absolute string
		indexes  []int
	}
	groups := map[string]*pathGroup{}
	for index, path := range paths {
		if !pending[index] {
			continue
		}
		absolute, relative, ok := r.repositoryPath(path)
		if !ok {
			result[index] = Attribution{State: Unavailable}
			continue
		}
		group := groups[relative]
		if group == nil {
			group = &pathGroup{absolute: absolute}
			groups[relative] = group
		}
		group.indexes = append(group.indexes, index)
	}

	relatives := make([]string, 0, len(groups))
	for relative := range groups {
		relatives = append(relatives, relative)
	}
	sort.Strings(relatives)
	uncommitted := r.uncommittedPaths(ctx, relatives)
	tracked := r.trackedPaths(ctx, relatives)
	for _, relative := range relatives {
		group := groups[relative]
		value := Attribution{}
		switch {
		case uncommitted[relative]:
			value.State = Uncommitted
		case !tracked[relative]:
			if _, err := os.Stat(group.absolute); err == nil {
				value.State = Uncommitted
			} else {
				value.State = Unavailable
			}
		default:
			value = r.resolveTracked(ctx, relative)
		}
		for _, index := range group.indexes {
			result[index] = value
		}
	}

	r.mu.Lock()
	for index, path := range paths {
		if pending[index] {
			r.cache[path] = result[index]
		}
	}
	r.mu.Unlock()
	return result
}

func (r *Resolver) repositoryPath(path string) (absolute, relative string, ok bool) {
	if r.err != nil || r.root == "" {
		return "", "", false
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", "", false
	}
	if resolved, resolveErr := filepath.EvalSymlinks(abs); resolveErr == nil {
		abs = resolved
	}
	rel, err := filepath.Rel(r.root, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", "", false
	}
	return abs, filepath.ToSlash(rel), true
}

func (r *Resolver) uncommittedPaths(ctx context.Context, paths []string) map[string]bool {
	result := map[string]bool{}
	for _, batch := range pathBatches(paths) {
		args := append([]string{"-C", r.root, "status", "--porcelain=v1", "-z", "--untracked-files=all", "--"}, batch...)
		output, err := r.run(ctx, args...)
		if err != nil {
			continue
		}
		for len(output) > 0 {
			end := bytes.IndexByte(output, 0)
			if end < 0 {
				break
			}
			record := output[:end]
			output = output[end+1:]
			if len(record) < 4 || record[2] != ' ' {
				continue
			}
			status, path := record[:2], string(record[3:])
			if bytes.Equal(status, []byte("??")) || status[0] == 'A' {
				result[path] = true
			}
			// Porcelain v1 -z emits a second NUL-terminated source path for
			// renames and copies. It has no status prefix of its own.
			if status[0] == 'R' || status[0] == 'C' {
				if sourceEnd := bytes.IndexByte(output, 0); sourceEnd >= 0 {
					output = output[sourceEnd+1:]
				}
			}
		}
	}
	return result
}

func (r *Resolver) trackedPaths(ctx context.Context, paths []string) map[string]bool {
	result := map[string]bool{}
	for _, batch := range pathBatches(paths) {
		args := append([]string{"-C", r.root, "ls-files", "-z", "--"}, batch...)
		output, err := r.run(ctx, args...)
		if err != nil {
			continue
		}
		for _, path := range bytes.Split(output, []byte{0}) {
			if len(path) > 0 {
				result[string(path)] = true
			}
		}
	}
	return result
}

func (r *Resolver) resolveTracked(ctx context.Context, relative string) Attribution {
	output, err := r.run(ctx, "-C", r.root, "log", "-1", "--follow", "--diff-filter=A", "--format=%H%x00%cN%x00%cE%x00%cI", "--", relative)
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

func (r *Resolver) run(ctx context.Context, args ...string) ([]byte, error) {
	if r.command != nil {
		return r.command(ctx, args...)
	}
	return execGitOutput(ctx, args...)
}

func execGitOutput(ctx context.Context, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, "git", args...).Output()
}

func pathBatches(paths []string) [][]string {
	const maximumPaths = 256
	var result [][]string
	for len(paths) > 0 {
		count := min(len(paths), maximumPaths)
		result = append(result, paths[:count])
		paths = paths[count:]
	}
	return result
}
