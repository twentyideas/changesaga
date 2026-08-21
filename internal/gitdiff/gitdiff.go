package gitdiff

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"net/url"
	"os"
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

type ReadOptions struct {
	AllowRepositoryMismatch bool
}

func Read(ctx context.Context, fromDir, repositoryURI, base, head string) (ChangeSet, error) {
	return ReadWithOptions(ctx, fromDir, repositoryURI, base, head, ReadOptions{})
}

func ReadWithOptions(ctx context.Context, fromDir, repositoryURI, base, head string, options ReadOptions) (ChangeSet, error) {
	repoOut, err := exec.CommandContext(ctx, "git", "-C", fromDir, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return ChangeSet{}, fmt.Errorf("locate Git repository: %w", err)
	}
	repo := strings.TrimSpace(string(repoOut))
	repositoryURI, err = diffuri.CanonicalRepository(repositoryURI)
	if err != nil {
		return ChangeSet{}, fmt.Errorf("canonicalize declared repository: %w", err)
	}
	if !options.AllowRepositoryMismatch {
		if err := VerifyRepository(ctx, repo, repositoryURI); err != nil {
			return ChangeSet{}, err
		}
	}
	baseCommit, err := resolveRevision(ctx, repo, base)
	if err != nil {
		return ChangeSet{}, err
	}

	var headCommit string
	if head == "WORKTREE" {
		headCommit, err = resolveRevision(ctx, repo, "HEAD")
	} else {
		headCommit, err = resolveRevision(ctx, repo, head)
	}
	if err != nil {
		return ChangeSet{}, err
	}
	mergeBase, err := resolveMergeBase(ctx, repo, baseCommit, headCommit)
	if err != nil {
		return ChangeSet{}, err
	}
	comparison := mergeBase
	if head != "WORKTREE" {
		comparison += ".." + headCommit
	}
	// Twenty lines gives the renderer useful expandable context while keeping
	// large comparisons bounded. These lines are not coverage atoms.
	args := canonicalDiffArgs(repo, "--unified=20", comparison, "--")
	cmd := exec.CommandContext(ctx, "git", args...)
	output, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if ok := errorAs(err, &exitErr); ok {
			return ChangeSet{}, fmt.Errorf("git diff: %s", strings.TrimSpace(string(exitErr.Stderr)))
		}
		return ChangeSet{}, fmt.Errorf("git diff: %w", err)
	}
	productArgs := canonicalDiffArgs(repo, "--binary", "--full-index", "--unified=3", comparison, "--", ".", ":(exclude,glob)**/*.saga/**")
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
	result := ChangeSet{Repository: repositoryURI, Base: base, Head: head, BaseOID: mergeBase, HeadOID: headIdentity, DisplayLines: displayLines}
	for _, atom := range atoms {
		reference := diffuri.Reference{Repository: repositoryURI, Base: mergeBase, Head: headIdentity, Kind: atom.Kind, Path: atom.Path, Side: atom.Side, Start: atom.Line, End: atom.Line, Event: atom.Event, OldPath: atom.OldPath, NewPath: atom.NewPath}
		if atom.Kind == "event" && atom.Event == "rename" {
			reference.Path = ""
		}
		atom.URI, err = diffuri.Build(reference)
		if err != nil {
			return ChangeSet{}, fmt.Errorf("build diff URI for %s: %w", atom.Key, err)
		}
		hasSagaPath, hasProductPath := classifyAtomPaths(atom)
		if hasSagaPath {
			result.SagaChanges = append(result.SagaChanges, atom)
		}
		if hasProductPath {
			result.Atoms = append(result.Atoms, atom)
		}
	}
	return result, nil
}

func canonicalDiffArgs(repo string, specific ...string) []string {
	args := []string{
		"-c", "core.quotePath=true",
		"-c", "diff.noprefix=false",
		"-c", "diff.srcPrefix=a/",
		"-c", "diff.dstPrefix=b/",
		"-c", "diff.submodule=short",
		"-c", "diff.algorithm=myers",
		"-c", "diff.indentHeuristic=false",
		"-c", "diff.renames=true",
		"-c", "diff.renameLimit=32767",
		"-C", repo, "diff",
		"--no-color", "--no-ext-diff", "--no-textconv", "--submodule=short", "--ignore-submodules=none",
		"--src-prefix=a/", "--dst-prefix=b/", "--diff-algorithm=myers", "--no-indent-heuristic", "--inter-hunk-context=0", "--find-renames=50%",
	}
	return append(args, specific...)
}

func classifyAtomPaths(atom Atom) (hasSaga, hasProduct bool) {
	for _, value := range []string{atom.Path, atom.OldPath, atom.NewPath} {
		if value == "" {
			continue
		}
		if IsSagaPath(value) {
			hasSaga = true
		} else {
			hasProduct = true
		}
	}
	return hasSaga, hasProduct
}

func resolveMergeBase(ctx context.Context, repo, base, head string) (string, error) {
	output, err := exec.CommandContext(ctx, "git", "-C", repo, "merge-base", "--", base, head).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("resolve merge base for %s and %s: %s", base, head, strings.TrimSpace(string(output)))
	}
	mergeBase := strings.TrimSpace(string(output))
	if mergeBase == "" {
		return "", fmt.Errorf("resolve merge base for %s and %s: no common ancestor", base, head)
	}
	return mergeBase, nil
}

// VerifyRepository confirms the checkout identity when its file path or origin
// provides enough information to do so.
func VerifyRepository(ctx context.Context, repo, declared string) error {
	declaredURL, _ := url.Parse(declared)
	if declaredURL.Scheme == "file" {
		declaredLocal, err := diffuri.RepositoryFilePath(declared)
		if err != nil {
			return fmt.Errorf("resolve declared file repository: %w", err)
		}
		declaredPath, err := filepath.EvalSymlinks(declaredLocal)
		if err != nil {
			return fmt.Errorf("verify declared file repository: %w", err)
		}
		actualPath, err := filepath.EvalSymlinks(repo)
		if err != nil {
			return fmt.Errorf("verify source checkout: %w", err)
		}
		if !samePath(declaredPath, actualPath) {
			return fmt.Errorf("source checkout %q does not match declared repository %q (use the explicit repository-mismatch override only when this checkout is known to be equivalent)", repo, declared)
		}
		return nil
	}
	remoteOutput, err := exec.CommandContext(ctx, "git", "-C", repo, "remote", "get-url", "origin").CombinedOutput()
	if err != nil || strings.TrimSpace(string(remoteOutput)) == "" {
		return fmt.Errorf("source checkout has no origin and cannot be verified against declared repository %q (use the explicit repository-mismatch override only when this checkout is known to be equivalent)", declared)
	}
	actual, err := normalizeRemote(strings.TrimSpace(string(remoteOutput)), repo)
	if err != nil {
		return fmt.Errorf("canonicalize source checkout origin: %w", err)
	}
	if !sameRepository(declared, actual) {
		return fmt.Errorf("source checkout origin %q does not match declared repository %q (use the explicit repository-mismatch override only when this checkout is known to be equivalent)", actual, declared)
	}
	return nil
}

func normalizeRemote(value, repo string) (string, error) {
	if at := strings.LastIndex(value, "@"); at >= 0 && !strings.Contains(value[:at], "://") {
		if colon := strings.Index(value[at:], ":"); colon > 0 {
			colon += at
			value = "ssh://" + value[at+1:colon] + "/" + value[colon+1:]
		}
	}
	if parsed, err := url.Parse(value); err == nil && parsed.IsAbs() {
		return diffuri.CanonicalRepository(value)
	}
	if !filepath.IsAbs(value) {
		value = filepath.Join(repo, value)
	}
	abs, err := filepath.Abs(value)
	if err != nil {
		return "", err
	}
	return diffuri.FileRepository(abs)
}

func sameRepository(left, right string) bool {
	leftURL, leftErr := url.Parse(left)
	rightURL, rightErr := url.Parse(right)
	if leftErr != nil || rightErr != nil {
		return false
	}
	if leftURL.Scheme == "file" || rightURL.Scheme == "file" || leftURL.Opaque != "" || rightURL.Opaque != "" {
		return left == right
	}
	leftPath := strings.TrimSuffix(strings.TrimSuffix(leftURL.Path, "/"), ".git")
	rightPath := strings.TrimSuffix(strings.TrimSuffix(rightURL.Path, "/"), ".git")
	return strings.EqualFold(leftURL.Hostname(), rightURL.Hostname()) && leftURL.Port() == rightURL.Port() && leftPath == rightPath
}

func samePath(left, right string) bool {
	if os.PathSeparator == '\\' {
		return strings.EqualFold(filepath.Clean(left), filepath.Clean(right))
	}
	return filepath.Clean(left) == filepath.Clean(right)
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
	var oldMode string
	var oldLine, newLine int
	inHunk := false
	fileHadAtom := false

	add := func(atom Atom) Atom {
		fileHadAtom = true
		atom.Key = Key(atom)
		if !seen[atom.Key] {
			seen[atom.Key] = true
			atoms = append(atoms, atom)
		}
		return atom
	}
	addEvent := func(event, path, oldPath, newPath string) {
		atom := add(Atom{Kind: "event", Event: event, Path: path, OldPath: oldPath, NewPath: newPath})
		displayLines = append(displayLines, DisplayLine{Kind: "event", Path: path, AtomKey: atom.Key, Event: event, OldPath: oldPath, NewPath: newPath})
	}
	flush := func() {
		if !fileHadAtom {
			path := preferredPath(newPath, oldPath)
			if path != "" {
				addEvent("modify", path, "", "")
			}
		}
	}

	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "diff --git "):
			flush()
			oldPath, newPath = parseDiffHeader(strings.TrimPrefix(line, "diff --git "))
			renameFrom = ""
			oldMode = ""
			fileHadAtom = false
			inHunk = false
		case strings.HasPrefix(line, "new file mode "):
			addEvent("add", preferredPath(newPath, oldPath), "", "")
			inHunk = false
		case strings.HasPrefix(line, "deleted file mode "):
			addEvent("delete", preferredPath(oldPath, newPath), "", "")
			inHunk = false
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
			addEvent("rename", to, renameFrom, to)
			inHunk = false
		case strings.HasPrefix(line, "old mode "):
			oldMode = strings.TrimSpace(strings.TrimPrefix(line, "old mode "))
			inHunk = false
		case strings.HasPrefix(line, "new mode "):
			newMode := strings.TrimSpace(strings.TrimPrefix(line, "new mode "))
			event := "mode"
			if modeType(oldMode) != modeType(newMode) {
				event = "type-change"
			}
			addEvent(event, preferredPath(newPath, oldPath), "", "")
			inHunk = false
		case strings.HasPrefix(line, "GIT binary patch") || strings.HasPrefix(line, "Binary files "):
			path := preferredPath(newPath, oldPath)
			addEvent("binary", path, "", "")
			inHunk = false
		case strings.HasPrefix(line, "@@ "):
			match := hunkPattern.FindStringSubmatch(line)
			if match == nil {
				return nil, nil, fmt.Errorf("parse hunk header %q", line)
			}
			oldLine, _ = strconv.Atoi(match[1])
			newLine, _ = strconv.Atoi(match[3])
			inHunk = true
		default:
			inHunk = false
		}
	}
	flush()
	if err := scanner.Err(); err != nil {
		return nil, nil, fmt.Errorf("scan Git diff: %w", err)
	}
	atoms, displayLines = coalesceTypeChanges(atoms, displayLines)
	return atoms, displayLines, nil
}

// Git represents some same-path type transitions (notably regular file to
// symlink or Gitlink) as a deletion followed by an addition. They form one
// lifecycle transition, not two independently coverable file identities.
func coalesceTypeChanges(atoms []Atom, displayLines []DisplayLine) ([]Atom, []DisplayLine) {
	type pair struct{ add, delete int }
	pairs := map[string]pair{}
	for i, atom := range atoms {
		if atom.Kind != "event" || atom.Event != "add" && atom.Event != "delete" {
			continue
		}
		value := pairs[atom.Path]
		if atom.Event == "add" {
			value.add = i + 1
		} else {
			value.delete = i + 1
		}
		pairs[atom.Path] = value
	}
	replacements := map[string]Atom{}
	removed := map[string]bool{}
	for path, pair := range pairs {
		if pair.add == 0 || pair.delete == 0 {
			continue
		}
		replacement := Atom{Kind: "event", Event: "type-change", Path: path}
		replacement.Key = Key(replacement)
		replacements[path] = replacement
		removed[atoms[pair.add-1].Key] = true
		removed[atoms[pair.delete-1].Key] = true
	}
	if len(replacements) == 0 {
		return atoms, displayLines
	}
	result := make([]Atom, 0, len(atoms))
	emitted := map[string]bool{}
	for _, atom := range atoms {
		if replacement, ok := replacements[atom.Path]; ok && removed[atom.Key] {
			if !emitted[atom.Path] {
				result = append(result, replacement)
				emitted[atom.Path] = true
			}
			continue
		}
		result = append(result, atom)
	}
	lines := make([]DisplayLine, 0, len(displayLines))
	emitted = map[string]bool{}
	for _, line := range displayLines {
		if replacement, ok := replacements[line.Path]; ok && removed[line.AtomKey] {
			if !emitted[line.Path] {
				lines = append(lines, DisplayLine{Kind: "event", Path: line.Path, AtomKey: replacement.Key, Event: replacement.Event})
				emitted[line.Path] = true
			}
			continue
		}
		lines = append(lines, line)
	}
	return result, lines
}

func modeType(mode string) string {
	if len(mode) < 3 {
		return mode
	}
	return mode[:3]
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
	var fallback int = -1
	for offset := 0; ; {
		separator := strings.Index(value[offset:], " b/")
		if separator < 0 {
			break
		}
		separator += offset
		fallback = separator
		oldPath := strings.TrimPrefix(value[:separator], "a/")
		newPath := strings.TrimPrefix(value[separator+1:], "b/")
		if oldPath == newPath {
			return oldPath, newPath
		}
		offset = separator + 1
	}
	if fallback < 0 {
		return "", ""
	}
	return strings.TrimPrefix(value[:fallback], "a/"), strings.TrimPrefix(value[fallback+1:], "b/")
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
