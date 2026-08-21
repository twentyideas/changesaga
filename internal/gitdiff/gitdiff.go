package gitdiff

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/change-saga/change-saga/internal/diffuri"
)

type Atom struct {
	Key     string `json:"key"`
	URI     string `json:"uri"`
	Kind    string `json:"kind"`
	Path    string `json:"path,omitempty"`
	Side    string `json:"side,omitempty"`
	Line    int    `json:"line,omitempty"`
	Content string `json:"content,omitempty"`
	Event   string `json:"event,omitempty"`
	OldPath string `json:"old_path,omitempty"`
	NewPath string `json:"new_path,omitempty"`
}

type ChangeSet struct {
	Repository  string `json:"repository"`
	Base        string `json:"base"`
	Head        string `json:"head"`
	BaseOID     string `json:"base_oid"`
	HeadOID     string `json:"head_oid"`
	Atoms       []Atom `json:"atoms"`
	SagaChanges []Atom `json:"saga_changes"`
	// DisplayLines contains bounded unchanged context alongside changed atoms.
	// It is renderer-only: coverage and persisted diff identity continue to use
	// Atoms exclusively.
	DisplayLines []DisplayLine `json:"-"`
}

type DisplayLine struct {
	Kind    string
	Path    string
	OldLine int
	NewLine int
	Content string
	AtomKey string
	Event   string
	OldPath string
	NewPath string
}

func Read(ctx context.Context, fromDir, repositoryURI, base, head string) (ChangeSet, error) {
	repoOut, err := exec.CommandContext(ctx, "git", "-C", fromDir, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return ChangeSet{}, fmt.Errorf("locate Git repository: %w", err)
	}
	repo := strings.TrimSpace(string(repoOut))
	baseCommit, err := resolveRevision(ctx, repo, base)
	if err != nil {
		return ChangeSet{}, err
	}

	comparison := baseCommit
	if head == "WORKTREE" {
		// The comparison remains base..worktree.
	} else {
		headCommit, err := resolveRevision(ctx, repo, head)
		if err != nil {
			return ChangeSet{}, err
		}
		comparison = baseCommit + "..." + headCommit
	}
	// Twenty lines gives the renderer useful expandable context while keeping
	// large comparisons bounded. These lines are not coverage atoms.
	args := []string{"-C", repo, "diff", "--unified=20", "--no-color", "--no-ext-diff", "--find-renames", comparison, "--"}
	cmd := exec.CommandContext(ctx, "git", args...)
	output, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if ok := errorAs(err, &exitErr); ok {
			return ChangeSet{}, fmt.Errorf("git diff: %s", strings.TrimSpace(string(exitErr.Stderr)))
		}
		return ChangeSet{}, fmt.Errorf("git diff: %w", err)
	}
	productArgs := []string{"-C", repo, "diff", "--binary", "--full-index", "--no-color", "--no-ext-diff", "--find-renames", comparison, "--", ".", ":(exclude,glob)**/*.saga/**"}
	productPatch, err := exec.CommandContext(ctx, "git", productArgs...).Output()
	if err != nil {
		return ChangeSet{}, fmt.Errorf("build product diff identity: %w", err)
	}
	digest := sha256.Sum256(productPatch)
	headIdentity := fmt.Sprintf("product-%x", digest[:])
	atoms, displayLines, err := parse(output)
	if err != nil {
		return ChangeSet{}, err
	}
	result := ChangeSet{Repository: repositoryURI, Base: base, Head: head, BaseOID: baseCommit, HeadOID: headIdentity, DisplayLines: displayLines}
	for _, atom := range atoms {
		reference := diffuri.Reference{Repository: repositoryURI, Base: baseCommit, Head: headIdentity, Kind: atom.Kind, Path: atom.Path, Side: atom.Side, Start: atom.Line, End: atom.Line, Event: atom.Event, OldPath: atom.OldPath, NewPath: atom.NewPath}
		atom.URI, err = diffuri.Build(reference)
		if err != nil {
			return ChangeSet{}, fmt.Errorf("build diff URI for %s: %w", atom.Key, err)
		}
		if IsSagaPath(atom.Path) || IsSagaPath(atom.OldPath) || IsSagaPath(atom.NewPath) {
			result.SagaChanges = append(result.SagaChanges, atom)
		} else {
			result.Atoms = append(result.Atoms, atom)
		}
	}
	return result, nil
}

func resolveRevision(ctx context.Context, repo, revision string) (string, error) {
	if strings.TrimSpace(revision) == "" {
		return "", fmt.Errorf("Git revision cannot be empty")
	}
	cmd := exec.CommandContext(ctx, "git", "-C", repo, "rev-parse", "--verify", "--end-of-options", revision+"^{commit}")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("resolve Git revision %q: %s", revision, strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}

// errorAs exists to keep the exec-specific error handling easy to test without
// exporting implementation details.
func errorAs(err error, target any) bool {
	switch value := target.(type) {
	case **exec.ExitError:
		exitErr, ok := err.(*exec.ExitError)
		if ok {
			*value = exitErr
		}
		return ok
	default:
		return false
	}
}

var hunkPattern = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

func Parse(patch []byte) ([]Atom, error) {
	atoms, _, err := parse(patch)
	return atoms, err
}

func parse(patch []byte) ([]Atom, []DisplayLine, error) {
	scanner := bufio.NewScanner(bytes.NewReader(patch))
	// Binary patch lines can be long.
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	var atoms []Atom
	var displayLines []DisplayLine
	seen := map[string]bool{}
	var oldPath, newPath, renameFrom string
	var oldLine, newLine int
	inHunk := false

	add := func(atom Atom) Atom {
		atom.Key = Key(atom)
		if !seen[atom.Key] {
			seen[atom.Key] = true
			atoms = append(atoms, atom)
		}
		return atom
	}

	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "diff --git "):
			oldPath, newPath = parseDiffHeader(strings.TrimPrefix(line, "diff --git "))
			renameFrom = ""
			inHunk = false
		case strings.HasPrefix(line, "--- "):
			oldPath = parseHeaderPath(strings.TrimPrefix(line, "--- "), "a/")
			inHunk = false
		case strings.HasPrefix(line, "+++ "):
			newPath = parseHeaderPath(strings.TrimPrefix(line, "+++ "), "b/")
			inHunk = false
		case strings.HasPrefix(line, "rename from "):
			renameFrom = unquoteGitPath(strings.TrimPrefix(line, "rename from "))
			oldPath = renameFrom
			inHunk = false
		case strings.HasPrefix(line, "rename to "):
			to := unquoteGitPath(strings.TrimPrefix(line, "rename to "))
			newPath = to
			atom := add(Atom{Kind: "event", Event: "rename", Path: to, OldPath: renameFrom, NewPath: to})
			displayLines = append(displayLines, DisplayLine{Kind: "event", Path: to, AtomKey: atom.Key, Event: atom.Event, OldPath: atom.OldPath, NewPath: atom.NewPath})
			inHunk = false
		case strings.HasPrefix(line, "old mode "):
			path := preferredPath(newPath, oldPath)
			atom := add(Atom{Kind: "event", Event: "mode", Path: path})
			displayLines = append(displayLines, DisplayLine{Kind: "event", Path: path, AtomKey: atom.Key, Event: atom.Event})
			inHunk = false
		case strings.HasPrefix(line, "GIT binary patch") || strings.HasPrefix(line, "Binary files "):
			path := preferredPath(newPath, oldPath)
			atom := add(Atom{Kind: "event", Event: "binary", Path: path, OldPath: oldPath, NewPath: newPath})
			displayLines = append(displayLines, DisplayLine{Kind: "event", Path: path, AtomKey: atom.Key, Event: atom.Event, OldPath: atom.OldPath, NewPath: atom.NewPath})
			inHunk = false
		case strings.HasPrefix(line, "@@ "):
			match := hunkPattern.FindStringSubmatch(line)
			if match == nil {
				return nil, nil, fmt.Errorf("parse hunk header %q", line)
			}
			oldLine, _ = strconv.Atoi(match[1])
			newLine, _ = strconv.Atoi(match[3])
			inHunk = true
		case inHunk && strings.HasPrefix(line, "-"):
			atom := add(Atom{Kind: "line", Path: oldPath, Side: "old", Line: oldLine, Content: strings.TrimPrefix(line, "-")})
			displayLines = append(displayLines, DisplayLine{Kind: "old", Path: oldPath, OldLine: oldLine, Content: atom.Content, AtomKey: atom.Key})
			oldLine++
		case inHunk && strings.HasPrefix(line, "+"):
			atom := add(Atom{Kind: "line", Path: newPath, Side: "new", Line: newLine, Content: strings.TrimPrefix(line, "+")})
			displayLines = append(displayLines, DisplayLine{Kind: "new", Path: newPath, NewLine: newLine, Content: atom.Content, AtomKey: atom.Key})
			newLine++
		case inHunk && strings.HasPrefix(line, " "):
			displayLines = append(displayLines, DisplayLine{Kind: "context", Path: preferredPath(newPath, oldPath), OldLine: oldLine, NewLine: newLine, Content: strings.TrimPrefix(line, " ")})
			oldLine++
			newLine++
		case inHunk && strings.HasPrefix(line, "\\ No newline"):
			// This marker does not represent a changed source line.
		default:
			inHunk = false
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, nil, fmt.Errorf("scan Git diff: %w", err)
	}
	return atoms, displayLines, nil
}

func Key(atom Atom) string {
	if atom.Kind == "event" {
		return fmt.Sprintf("event:%s:%s:%s:%s", atom.Event, atom.Path, atom.OldPath, atom.NewPath)
	}
	return fmt.Sprintf("line:%s:%s:%d", atom.Path, atom.Side, atom.Line)
}

func IsSagaPath(path string) bool {
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		if strings.HasSuffix(part, ".saga") {
			return true
		}
	}
	return false
}

func parseHeaderPath(value, prefix string) string {
	if value == "/dev/null" {
		return ""
	}
	if tab := strings.IndexByte(value, '\t'); tab >= 0 {
		value = value[:tab]
	}
	value = unquoteGitPath(value)
	return strings.TrimPrefix(value, prefix)
}

func parseDiffHeader(value string) (string, string) {
	if strings.HasPrefix(value, `"`) {
		first, rest, ok := takeQuoted(value)
		if !ok {
			return "", ""
		}
		rest = strings.TrimSpace(rest)
		second, _, ok := takeQuoted(rest)
		if !ok {
			return "", ""
		}
		return strings.TrimPrefix(first, "a/"), strings.TrimPrefix(second, "b/")
	}
	separator := strings.LastIndex(value, " b/")
	if separator < 0 {
		return "", ""
	}
	return strings.TrimPrefix(value[:separator], "a/"), strings.TrimPrefix(value[separator+1:], "b/")
}

func takeQuoted(value string) (string, string, bool) {
	if !strings.HasPrefix(value, `"`) {
		return "", value, false
	}
	escaped := false
	for i := 1; i < len(value); i++ {
		switch {
		case escaped:
			escaped = false
		case value[i] == '\\':
			escaped = true
		case value[i] == '"':
			unquoted, err := strconv.Unquote(value[:i+1])
			if err != nil {
				return "", value, false
			}
			return unquoted, value[i+1:], true
		}
	}
	return "", value, false
}

func unquoteGitPath(value string) string {
	if strings.HasPrefix(value, `"`) {
		if unquoted, err := strconv.Unquote(value); err == nil {
			return unquoted
		}
	}
	return value
}

func preferredPath(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
