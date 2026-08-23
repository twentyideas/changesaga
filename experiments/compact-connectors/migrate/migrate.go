// Package migrate converts a Change Saga v2 evidence tree to the experimental
// connector encoding and back.
//
// Both directions work on a copy. Neither ever writes into the saga it reads,
// so a migration can be evaluated against a read-only fixture and thrown away.
package migrate

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/twentyideas/changesaga/experiments/compact-connectors/connector"
	"github.com/twentyideas/changesaga/internal/saga"
)

// Owner is one narrative element that can own evidence, together with the
// saga-root-relative directory its evidence lives in.
type Owner struct {
	Target string
	Kind   string
	// Dir is the owning package directory, e.g.
	// "backend.chapter/overview.fragment/___landmarks/submit.landmark". The
	// saga root itself has an empty Dir.
	Dir   string
	Diffs []saga.DiffFile

	attach func([]saga.DiffFile)
}

// Attach replaces the owner's evidence on the loaded document. A connector
// aware reader uses it to hand decoded shards to the unmodified coverage
// evaluator, so connector evidence and v2 evidence go through exactly one
// ownership, overlap, and stale-reference implementation.
func (o Owner) Attach(files []saga.DiffFile) {
	if o.attach != nil {
		o.attach(files)
	}
}

// EvidenceDir is where this owner's evidence records live.
func (o Owner) EvidenceDir() string {
	if o.Dir == "" {
		return connector.EvidenceDirectory
	}
	return path.Join(o.Dir, connector.EvidenceDirectory)
}

// Owners walks a loaded saga and returns every evidence owner in a stable
// order, including owners that currently own nothing. It reads the loaded
// document rather than the filesystem, so the owner set is exactly the one
// coverage evaluation uses and cannot drift from it.
func Owners(document *saga.Saga) []Owner {
	var owners []Owner
	walk(document.Section, func(section *saga.Section) {
		kind, dir := section.Kind, section.Path
		if section == document.Section {
			kind, dir = "saga", ""
		}
		owners = append(owners, Owner{
			Target: section.Target, Kind: kind, Dir: dir, Diffs: section.Diffs,
			attach: func(files []saga.DiffFile) { section.Diffs = files },
		})
		for _, fragment := range section.Fragments {
			owners = append(owners, Owner{
				Target: fragment.Target, Kind: "fragment", Dir: fragment.Path, Diffs: fragment.Diffs,
				attach: func(files []saga.DiffFile) { fragment.Diffs = files },
			})
			for i := range fragment.Landmarks {
				landmark := &fragment.Landmarks[i]
				owners = append(owners, Owner{
					Target: landmark.Target, Kind: "landmark",
					Dir: path.Dir(landmark.Path), Diffs: landmark.Diffs,
					attach: func(files []saga.DiffFile) { landmark.Diffs = files },
				})
			}
		}
	})
	sort.Slice(owners, func(i, j int) bool { return owners[i].Target < owners[j].Target })
	return owners
}

func walk(section *saga.Section, fn func(*saga.Section)) {
	fn(section)
	for _, child := range section.Children {
		walk(child, fn)
	}
}

// ShardName is the connector file name for one source path. It is a pure
// function of the path, so two authors who cover the same file of the same
// target write the same name and Git merges their record lines instead of
// leaving two near-identical files behind.
func ShardName(sourcePath string) string {
	sum := sha256.Sum256([]byte(sourcePath))
	digest := hex.EncodeToString(sum[:4])

	slug := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			return r
		case r >= 'A' && r <= 'Z':
			return r + ('a' - 'A')
		default:
			return '-'
		}
	}, sourcePath)
	for strings.Contains(slug, "--") {
		slug = strings.ReplaceAll(slug, "--", "-")
	}
	slug = strings.Trim(slug, "-")
	if len(slug) > 48 {
		// Keep the tail: the file name is the distinguishing part of a path.
		// Drop the leading partial segment so the name never starts mid-word.
		slug = slug[len(slug)-48:]
		if cut := strings.Index(slug, "-"); cut >= 0 && cut < len(slug)-1 {
			slug = slug[cut+1:]
		}
	}
	if slug == "" {
		slug = "path"
	}
	return slug + "-" + digest + connector.Extension
}

// Shard groups one owner's evidence into per-source-path connector files.
func Shard(owner Owner, granularity connector.Granularity) ([]connector.File, error) {
	bySource := map[string][]connector.Record{}
	for _, diffFile := range owner.Diffs {
		for _, reference := range diffFile.Diffs {
			record, err := connector.FromReference(reference)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", diffFile.Path, err)
			}
			source := record.SourcePath()
			bySource[source] = append(bySource[source], record)
		}
	}
	sources := make([]string, 0, len(bySource))
	for source := range bySource {
		sources = append(sources, source)
	}
	sort.Strings(sources)

	files := make([]connector.File, 0, len(sources))
	for _, source := range sources {
		records := dedupe(bySource[source])
		if granularity == connector.Ranges {
			records = connector.Coalesce(records)
		} else {
			connector.SortRecords(records)
		}
		files = append(files, connector.File{Owner: owner.Target, Source: source, Records: records})
	}
	return files, nil
}

// dedupe removes records identical in every field. Two v2 evidence files may
// name the same atom, and that overlap is reported by coverage, but a repeat
// inside one owner with one note is pure duplication whose removal changes no
// ownership.
func dedupe(records []connector.Record) []connector.Record {
	seen := make(map[connector.Record]bool, len(records))
	out := make([]connector.Record, 0, len(records))
	for _, record := range records {
		if seen[record] {
			continue
		}
		seen[record] = true
		out = append(out, record)
	}
	return out
}

// Mode selects what a forward migration leaves behind.
type Mode int

const (
	// Connectors drops the v2 JSON evidence and writes connector shards only.
	// The result is smaller but readable only by a connector-aware engine.
	Connectors Mode = iota
	// Dual keeps the v2 JSON evidence and adds connector shards beside it. A
	// v2 reader ignores the connector files and reports exactly the coverage it
	// reported before, which makes this the safe transition state.
	Dual
)

// Result reports what a migration produced.
type Result struct {
	Owners         int
	ConnectorFiles int
	ConnectorBytes int64
	HeaderBytes    int64
	Records        int
	LegacyFiles    int
	LegacyBytes    int64
	LegacyRefs     int
}

// ToConnectors copies a v2 saga to destination and encodes its evidence as
// connector shards. The source tree is only read.
func ToConnectors(sourceRoot, destinationRoot string, granularity connector.Granularity, mode Mode) (Result, error) {
	document, validation, err := saga.Load(sourceRoot)
	if err != nil {
		return Result{}, err
	}
	if !validation.Valid {
		return Result{}, fmt.Errorf("saga is invalid; migrate only a saga that already loads cleanly")
	}
	excluded := connector.EvidenceDirectory
	if mode == Dual {
		excluded = ""
	}
	if err := copyTreeExcluding(sourceRoot, destinationRoot, excluded); err != nil {
		return Result{}, err
	}

	var result Result
	for _, owner := range Owners(document) {
		if len(owner.Diffs) == 0 {
			continue
		}
		result.Owners++
		for _, diffFile := range owner.Diffs {
			result.LegacyFiles++
			result.LegacyRefs += len(diffFile.Diffs)
			if info, statErr := os.Stat(filepath.Join(sourceRoot, filepath.FromSlash(diffFile.Path))); statErr == nil {
				result.LegacyBytes += info.Size()
			}
		}
		files, err := Shard(owner, granularity)
		if err != nil {
			return Result{}, err
		}
		dir := filepath.Join(destinationRoot, filepath.FromSlash(owner.EvidenceDir()))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return Result{}, err
		}
		for _, file := range files {
			target := filepath.Join(dir, ShardName(file.Source))
			handle, err := os.Create(target)
			if err != nil {
				return Result{}, err
			}
			header, total, err := connector.WriteSized(handle, file)
			if err != nil {
				handle.Close()
				return Result{}, err
			}
			if err := handle.Close(); err != nil {
				return Result{}, err
			}
			result.ConnectorFiles++
			result.ConnectorBytes += total
			result.HeaderBytes += header
			result.Records += len(file.Records)
		}
	}
	return result, nil
}

// ToLegacy is the reverse migration: it copies a connector saga to destination
// and writes one v2 `___diffs` JSON record per connector shard, so a saga that
// adopted connectors can always be handed back to a v2-only reader.
func ToLegacy(sourceRoot, destinationRoot string, granularity connector.Granularity) (Result, error) {
	if err := copyTreeExcluding(sourceRoot, destinationRoot, connector.EvidenceDirectory); err != nil {
		return Result{}, err
	}
	var result Result
	owners := map[string]bool{}
	err := filepath.WalkDir(sourceRoot, func(current string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || filepath.Ext(entry.Name()) != connector.Extension {
			return nil
		}
		if filepath.Base(filepath.Dir(current)) != connector.EvidenceDirectory {
			return nil
		}
		file, err := ReadShard(current)
		if err != nil {
			return err
		}
		references, err := file.References(granularity)
		if err != nil {
			return fmt.Errorf("%s: %w", current, err)
		}
		rel, err := filepath.Rel(sourceRoot, filepath.Dir(current))
		if err != nil {
			return err
		}
		dir := filepath.Join(destinationRoot, rel)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		name := strings.TrimSuffix(entry.Name(), connector.Extension) + ".json"
		written, err := writeLegacyRecord(filepath.Join(dir, name), references)
		if err != nil {
			return err
		}
		result.LegacyFiles++
		result.LegacyBytes += written
		result.LegacyRefs += len(references)
		result.Records += len(file.Records)
		owners[file.Owner] = true
		return nil
	})
	result.Owners = len(owners)
	return result, err
}

// ReadShard parses one connector file from disk.
func ReadShard(path string) (connector.File, error) {
	handle, err := os.Open(path)
	if err != nil {
		return connector.File{}, err
	}
	defer handle.Close()
	file, err := connector.Parse(handle)
	if err != nil {
		return connector.File{}, fmt.Errorf("%s: %w", path, err)
	}
	return file, nil
}

func copyTreeExcluding(sourceRoot, destinationRoot, excludedDirectory string) error {
	if err := os.MkdirAll(destinationRoot, 0o755); err != nil {
		return err
	}
	return filepath.WalkDir(sourceRoot, func(current string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(sourceRoot, current)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if entry.IsDir() {
			if excludedDirectory != "" && entry.Name() == excludedDirectory {
				return filepath.SkipDir
			}
			return os.MkdirAll(filepath.Join(destinationRoot, rel), 0o755)
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		return copyFile(current, filepath.Join(destinationRoot, rel))
	})
}

func copyFile(source, destination string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	out, err := os.Create(destination)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
