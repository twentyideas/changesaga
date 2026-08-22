package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/twentyideas/changesaga/internal/diffuri"
	"github.com/twentyideas/changesaga/internal/gitdiff"
	"github.com/twentyideas/changesaga/internal/saga"
	"github.com/twentyideas/changesaga/internal/store"
)

// coverRecord is one coverage instruction. Its fields are the batch spelling of
// the cover flags, so an author who already knows the flags does not learn a
// second vocabulary to use stdin. A record maps exact atoms exactly as a single
// invocation does; batching changes how many instructions are delivered, never
// how precisely each one selects.
type coverRecord struct {
	Target       string   `json:"target,omitempty"`
	Path         string   `json:"path,omitempty"`
	Side         string   `json:"side,omitempty"`
	Lines        string   `json:"lines,omitempty"`
	ChangedLines bool     `json:"changed_lines,omitempty"`
	Event        string   `json:"event,omitempty"`
	OldPath      string   `json:"old_path,omitempty"`
	NewPath      string   `json:"new_path,omitempty"`
	Note         string   `json:"note,omitempty"`
	Name         string   `json:"name,omitempty"`
	URIs         []string `json:"uris,omitempty"`
}

// plannedRecord is a fully resolved write that has not happened yet. Planning
// every record before the first write is what makes a batch all-or-nothing: a
// record that cannot be resolved fails the whole batch while the saga is still
// untouched.
type plannedRecord struct {
	targetID string
	file     saga.DiffFile
	dir      string
	path     string
	relative string
}

type coverageMutationOutput struct {
	OK            bool     `json:"ok"`
	DryRun        bool     `json:"dry_run"`
	Records       int      `json:"records"`
	Selectors     int      `json:"selectors"`
	EvidenceFiles []string `json:"evidence_files"`
}

const maxGeneratedNameAttempts = 1000

// Cover attaches diff evidence to a narrative target. os.Stdin is bound here
// rather than read inside the command so tests drive --batch deterministically.
func Cover(ctx context.Context, args []string, out io.Writer) error {
	err := cover(ctx, args, out, os.Stdin)
	if err != nil && jsonFlagRequested(args) {
		return reportJSONMutationFailure(out, err)
	}
	return err
}

func cover(ctx context.Context, args []string, out io.Writer, stdin io.Reader) error {
	flags := commandFlags("cover", commandUsage["cover"], out)
	target := flags.String("target", ".", "section, .fragment, or landmark target receiving evidence; accepts <fragment>#<landmark-id>")
	repoDir := flags.String("repo", "", "source repository checkout; required when separate")
	path := flags.String("path", "", "changed repository path")
	side := flags.String("side", "", "line side: old or new")
	lines := flags.String("lines", "", "line ranges, for example 4-9,12")
	changedLines := flags.Bool("changed-lines", false, "select every exact changed line and file event for --path; optionally filter lines with --side")
	event := flags.String("event", "", "file event: add, delete, type-change, rename, mode, binary, or modify")
	oldPath := flags.String("old-path", "", "old path for a rename event")
	newPath := flags.String("new-path", "", "new path for a rename event")
	note := flags.String("note", "", "optional explanation for report authors")
	name := flags.String("name", "", "coverage filename without .json")
	batch := flags.String("batch", "", "read coverage records from a JSON file, or - for stdin")
	dryRun := flags.Bool("dry-run", false, "resolve and report the coverage records without writing them")
	jsonOutput := flags.Bool("json", false, "emit one machine-readable summary instead of every selector")
	quiet := flags.Bool("quiet", false, "suppress successful output")
	allowRepositoryMismatch := flags.Bool("allow-repository-mismatch", false, "use a checkout whose origin differs from the declared repository")
	var uris stringList
	flags.Var(&uris, "uri", "absolute saga-diff URI; repeatable")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return fmt.Errorf("usage: %s", commandUsage["cover"])
	}
	if *jsonOutput && *quiet {
		return fmt.Errorf("--json and --quiet cannot be combined")
	}
	document, _, err := saga.Load(flags.Arg(0))
	if err != nil {
		return err
	}

	records, err := coverRecords(*batch, stdin, coverRecord{
		Target: *target, Path: *path, Side: *side, Lines: *lines, ChangedLines: *changedLines, Event: *event,
		OldPath: *oldPath, NewPath: *newPath, Note: *note, Name: *name, URIs: uris,
	}, flags)
	if err != nil {
		return err
	}

	// The Git comparison is read once for the whole batch, and before the lock
	// is taken, so a slow diff neither repeats per record nor stalls other
	// writers. Only target resolution and the record writes are serialized.
	var changes *gitdiff.ChangeSet
	for _, record := range records {
		if record.Path == "" && record.Event == "" && !record.ChangedLines {
			continue
		}
		checkout := firstNonEmpty(*repoDir, document.Root)
		read, readErr := gitdiff.ReadWithOptions(ctx, checkout, document.Manifest.Source.Repository, document.Manifest.Source.Base, document.Manifest.Source.Head, gitdiff.ReadOptions{AllowRepositoryMismatch: *allowRepositoryMismatch})
		if readErr != nil {
			return fmt.Errorf("read source diff (use --repo for a separate saga repository): %w", readErr)
		}
		changes = &read
		break
	}

	repository, err := diffuri.CanonicalRepository(document.Manifest.Source.Repository)
	if err != nil {
		return fmt.Errorf("invalid declared source repository: %w", err)
	}
	files := make([]saga.DiffFile, len(records))
	for i, record := range records {
		file, buildErr := buildCoverageFile(record, changes, repository)
		if buildErr != nil {
			return recordError(records, i, buildErr)
		}
		files[i] = file
	}

	now := time.Now()
	var planned []plannedRecord
	plan := func(locked *saga.Saga) error {
		var planErr error
		planned, planErr = planCoverage(locked, records, files, now)
		return planErr
	}
	if *dryRun {
		// A dry run still resolves targets and names against the real saga so
		// its report matches what a real run would do, but it takes no lock and
		// creates nothing.
		if err := plan(document); err != nil {
			return err
		}
		if *quiet {
			return nil
		}
		if *jsonOutput {
			return writeJSON(out, coverageOutput(planned, true))
		}
		for _, record := range planned {
			fmt.Fprintf(out, "Would add %s (%s)\n", record.relative, record.targetID)
			for _, reference := range record.file.Diffs {
				fmt.Fprintf(out, "  %s\n", reference.URI)
			}
		}
		fmt.Fprintf(out, "Dry run: %d coverage record(s) resolved, nothing written\n", len(planned))
		return nil
	}

	if err := authorMutation(flags.Arg(0), func(locked *saga.Saga) error {
		if err := plan(locked); err != nil {
			return err
		}
		if err := ensureCoverageDirectories(locked.Root, planned); err != nil {
			return err
		}
		return writeCoverage(planned)
	}); err != nil {
		return err
	}
	if *quiet {
		return nil
	}
	if *jsonOutput {
		return writeJSON(out, coverageOutput(planned, false))
	}
	for _, record := range planned {
		fmt.Fprintf(out, "Added %s\n", record.relative)
	}
	return nil
}

func coverageOutput(planned []plannedRecord, dryRun bool) coverageMutationOutput {
	result := coverageMutationOutput{OK: true, DryRun: dryRun, Records: len(planned), EvidenceFiles: make([]string, 0, len(planned))}
	for _, record := range planned {
		result.EvidenceFiles = append(result.EvidenceFiles, record.relative)
		result.Selectors += len(record.file.Diffs)
	}
	return result
}

// coverRecords returns the batch records, or the single record built from the
// flags. Per-record flags are rejected alongside --batch instead of silently
// losing to the file, because a dropped selector would quietly under-cover.
func coverRecords(batch string, stdin io.Reader, single coverRecord, flags *flag.FlagSet) ([]coverRecord, error) {
	if batch == "" {
		if len(single.URIs) == 0 && single.Path == "" && single.Event == "" && !single.ChangedLines {
			return nil, fmt.Errorf("provide --uri or --path/--lines (or --event), or --batch for many records at once")
		}
		return []coverRecord{single}, nil
	}
	for _, conflicting := range []string{"path", "side", "lines", "changed-lines", "event", "old-path", "new-path", "name", "uri"} {
		if flagWasSet(flags, conflicting) {
			return nil, fmt.Errorf("--%s cannot be combined with --batch; put it in the batch record instead", conflicting)
		}
	}
	data, err := readBatch(batch, stdin)
	if err != nil {
		return nil, err
	}
	records, err := parseCoverRecords(data)
	if err != nil {
		return nil, err
	}
	// --target and --note stay usable as batch-wide defaults: a batch is
	// usually many selectors for one narrative target.
	for i := range records {
		if strings.TrimSpace(records[i].Target) == "" {
			records[i].Target = single.Target
		}
		if records[i].Note == "" {
			records[i].Note = single.Note
		}
	}
	return records, nil
}

func readBatch(source string, stdin io.Reader) ([]byte, error) {
	if source == "-" {
		if stdin == nil {
			return nil, errors.New("--batch - requires records on standard input")
		}
		return io.ReadAll(stdin)
	}
	return os.ReadFile(source)
}

// parseCoverRecords accepts newline-delimited JSON objects or a single JSON
// array of them. Unknown fields are rejected: a misspelled "lines" would
// otherwise be dropped and produce evidence that silently covers nothing.
func parseCoverRecords(data []byte) ([]coverRecord, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil, errors.New("--batch input contained no coverage records")
	}
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.DisallowUnknownFields()
	var records []coverRecord
	if trimmed[0] == '[' {
		if err := decoder.Decode(&records); err != nil {
			return nil, fmt.Errorf("parse --batch records: %w", err)
		}
	} else {
		for {
			var record coverRecord
			err := decoder.Decode(&record)
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return nil, fmt.Errorf("parse --batch record %d: %w", len(records)+1, err)
			}
			records = append(records, record)
		}
	}
	if err := decoder.Decode(new(json.RawMessage)); !errors.Is(err, io.EOF) {
		return nil, errors.New("--batch input contained trailing data after the last record")
	}
	if len(records) == 0 {
		return nil, errors.New("--batch input contained no coverage records")
	}
	return records, nil
}

// buildCoverageFile turns one record into the diff file that will be written.
// Selector construction is identical to the single-invocation path, so batching
// cannot widen a mapping.
func buildCoverageFile(record coverRecord, changes *gitdiff.ChangeSet, repository string) (saga.DiffFile, error) {
	uris := append([]string(nil), record.URIs...)
	for _, value := range uris {
		reference, err := diffuri.Parse(value)
		if err != nil {
			return saga.DiffFile{}, fmt.Errorf("invalid --uri: %w", err)
		}
		if reference.Repository != repository {
			return saga.DiffFile{}, fmt.Errorf("invalid --uri: repository %q does not match saga source repository %q", reference.Repository, repository)
		}
	}
	if record.ChangedLines {
		if record.Path == "" {
			return saga.DiffFile{}, errors.New("--changed-lines requires --path")
		}
		if record.Lines != "" || record.Event != "" || record.OldPath != "" || record.NewPath != "" || len(record.URIs) != 0 {
			return saga.DiffFile{}, errors.New("--changed-lines cannot be combined with --lines, --event, --old-path, --new-path, or --uri")
		}
		if record.Side != "" && record.Side != "old" && record.Side != "new" {
			return saga.DiffFile{}, errors.New("--side must be old or new when used with --changed-lines")
		}
		if changes == nil {
			return saga.DiffFile{}, errors.New("--changed-lines requires the source comparison")
		}
		path := filepath.ToSlash(record.Path)
		for _, atom := range changes.Atoms {
			matchesPath := atom.Path == path || atom.OldPath == path || atom.NewPath == path
			if !matchesPath || atom.Kind == "line" && record.Side != "" && atom.Side != record.Side {
				continue
			}
			uris = append(uris, atom.URI)
		}
		if len(uris) == 0 {
			return saga.DiffFile{}, fmt.Errorf("--path %q has no changed atoms%s", path, map[bool]string{true: " on side " + record.Side, false: ""}[record.Side != ""])
		}
	} else if record.Path != "" || record.Event != "" {
		if changes == nil {
			return saga.DiffFile{}, errors.New("path and event coverage require the source comparison")
		}
		if record.Event == "" {
			if record.Side != "old" && record.Side != "new" {
				return saga.DiffFile{}, errors.New("line coverage requires side old or new")
			}
			ranges, err := parseRanges(record.Lines)
			if err != nil {
				return saga.DiffFile{}, err
			}
			for _, lineRange := range ranges {
				value, err := diffuri.Build(diffuri.Reference{Repository: changes.Repository, Base: changes.BaseOID, Head: changes.HeadOID, Kind: "line", Path: filepath.ToSlash(record.Path), Side: record.Side, Start: lineRange.Start, End: lineRange.End})
				if err != nil {
					return saga.DiffFile{}, err
				}
				uris = append(uris, value)
			}
		} else {
			value, err := diffuri.Build(diffuri.Reference{Repository: changes.Repository, Base: changes.BaseOID, Head: changes.HeadOID, Kind: "event", Event: record.Event, Path: filepath.ToSlash(record.Path), OldPath: filepath.ToSlash(record.OldPath), NewPath: filepath.ToSlash(record.NewPath)})
			if err != nil {
				return saga.DiffFile{}, err
			}
			uris = append(uris, value)
		}
	}
	if len(uris) == 0 {
		return saga.DiffFile{}, errors.New("provide uris or path/lines (or event)")
	}
	file := saga.DiffFile{Version: saga.CurrentVersion}
	for _, value := range uris {
		file.Diffs = append(file.Diffs, saga.DiffReference{URI: value, Note: record.Note})
	}
	return file, nil
}

// planCoverage resolves every target and destination filename up front. Names
// are reserved across the whole batch, so two records in one batch collide with
// each other exactly as loudly as one record collides with a record already on
// disk.
func planCoverage(document *saga.Saga, records []coverRecord, files []saga.DiffFile, now time.Time, replaceable ...string) ([]plannedRecord, error) {
	planned := make([]plannedRecord, 0, len(records))
	claimed := map[string]int{}
	allowed := map[string]bool{}
	for _, path := range replaceable {
		allowed[canonicalCoveragePath(path)] = true
	}
	for i, record := range records {
		targetDir, targetID, err := resolveTarget(document, record.Target, true)
		if err != nil {
			return nil, recordError(records, i, err)
		}
		diffDir := filepath.Join(targetDir, "___diffs")
		name, err := coverageName(record, diffDir, claimed, allowed, now)
		if err != nil {
			return nil, recordError(records, i, err)
		}
		full := filepath.Join(diffDir, name+".json")
		claimed[full] = i
		relative, _ := filepath.Rel(document.Root, full)
		planned = append(planned, plannedRecord{
			targetID: targetID, file: files[i], dir: diffDir,
			path: full, relative: filepath.ToSlash(relative),
		})
	}
	return planned, nil
}

// ensureCoverageDirectories runs only after every record, target, selector,
// and destination name has passed preflight. If directory creation itself
// fails, any empty directories created by this call are removed so a rejected
// batch has no observable filesystem residue.
func ensureCoverageDirectories(root string, planned []plannedRecord) error {
	ensured := map[string]string{}
	var created []string
	for index := range planned {
		dir := planned[index].dir
		if canonical, ok := ensured[dir]; ok {
			planned[index].dir = canonical
			planned[index].path = filepath.Join(canonical, filepath.Base(planned[index].path))
			continue
		}
		_, statErr := os.Lstat(dir)
		canonical, err := store.EnsureDirWithin(root, dir)
		if err != nil {
			for createdIndex := len(created) - 1; createdIndex >= 0; createdIndex-- {
				_ = os.Remove(created[createdIndex])
			}
			return err
		}
		if os.IsNotExist(statErr) {
			created = append(created, canonical)
		}
		ensured[dir] = canonical
		planned[index].dir = canonical
		planned[index].path = filepath.Join(canonical, filepath.Base(planned[index].path))
	}
	return nil
}

// coverageName picks the record filename. An explicit name is an author's
// stable handle, so a collision is reported instead of renamed; a generated
// name has no meaning to the author, so it is uniquified deterministically.
func coverageName(record coverRecord, dir string, claimed map[string]int, replaceable map[string]bool, now time.Time) (string, error) {
	if strings.TrimSpace(record.Name) != "" {
		name := store.Slug(record.Name)
		full := filepath.Join(dir, name+".json")
		if other, taken := claimed[full]; taken {
			return "", fmt.Errorf("coverage name %q collides with record %d, which also writes %s", record.Name, other+1, filepath.Base(full))
		}
		if _, err := os.Lstat(full); err == nil {
			if !replaceable[canonicalCoveragePath(full)] {
				hint := ""
				if name != record.Name {
					hint = fmt.Sprintf(" (name %q is stored as %q)", record.Name, name)
				}
				return "", fmt.Errorf("coverage record %s already exists%s; choose a different name or omit it to generate one", filepath.Base(full), hint)
			}
		} else if !os.IsNotExist(err) {
			return "", err
		}
		return name, nil
	}
	return uniqueGeneratedName(dir, generatedCoverageName(record, now), claimed)
}

// canonicalCoveragePath resolves existing parent symlinks without following
// the final file. macOS exposes /var through /private/var, so plain absolute
// strings can name the same evidence record differently and make a same-name
// replacement delete the file it just wrote.
func canonicalCoveragePath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(abs))
	if err != nil {
		return abs
	}
	return filepath.Join(parent, filepath.Base(abs))
}

// uniqueGeneratedName resolves a generated base to a free filename by appending
// -2, -3, and so on. The sequence is deterministic so a collision produces a
// predictable name rather than a second random draw.
func uniqueGeneratedName(dir, base string, claimed map[string]int) (string, error) {
	for attempt := 1; attempt <= maxGeneratedNameAttempts; attempt++ {
		candidate := base
		if attempt > 1 {
			candidate = fmt.Sprintf("%s-%d", base, attempt)
		}
		full := filepath.Join(dir, candidate+".json")
		if _, taken := claimed[full]; taken {
			continue
		}
		if _, err := os.Lstat(full); os.IsNotExist(err) {
			return candidate, nil
		} else if err != nil {
			return "", err
		}
	}
	return "", fmt.Errorf("could not find an unused coverage record name for %q", base)
}

// generatedCoverageName keeps the uniquifying event ID intact. Slugging the
// joined string would truncate it to 60 characters, and for any path longer
// than a couple of directories that truncation removed the random suffix
// entirely, so two records for the same file collided.
func generatedCoverageName(record coverRecord, now time.Time) string {
	prefix := store.Slug(firstNonEmpty(record.Path, record.Event, "diff"))
	if len(prefix) > 20 {
		prefix = strings.Trim(prefix[:20], "-")
	}
	if prefix == "" {
		prefix = "diff"
	}
	return prefix + "-" + store.Slug(store.EventID(now))
}

// writeCoverage publishes a planned batch. Every destination was proven free
// during planning, so a failure here is an I/O fault; the records already
// written are removed rather than left as a partial batch.
func writeCoverage(planned []plannedRecord) error {
	for i, record := range planned {
		if err := store.WriteJSON(record.path, record.file, true); err != nil {
			for _, written := range planned[:i] {
				_ = os.Remove(written.path)
				_ = store.SyncDir(written.dir)
			}
			if len(planned) == 1 {
				return err
			}
			return fmt.Errorf("write coverage record %d of %d: %w; no records were kept", i+1, len(planned), err)
		}
	}
	return nil
}

func recordError(records []coverRecord, index int, err error) error {
	if len(records) == 1 {
		return err
	}
	return fmt.Errorf("batch record %d of %d: %w", index+1, len(records), err)
}
