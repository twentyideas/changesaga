// Package connector implements an experimental, merge-friendly text encoding
// for Change Saga coverage evidence.
//
// A Change Saga v2 evidence record (`___diffs/*.json`) stores one fully
// realized `saga-diff://` URI per changed atom. Every URI repeats the
// repository identity, the base OID, the product head identity, and the source
// path, and every reference repeats the authored note. On a whole-codebase
// saga that repetition dominates: the linked fixture spends 240 MB restating
// the same comparison identity 1.6 million times.
//
// A connector file is the same evidence with the repeated parts hoisted into a
// small header and the per-atom parts written as sorted, contiguous ranges. One
// file covers exactly one (ownership target, source path) pair, so two authors
// documenting different files of the same change never write to the same file.
//
// The encoding is deliberately line oriented and canonically sorted. Git's
// three-way merge is a line merge; a format whose records are one per line and
// whose ordering is a pure function of content merges without an opaque
// conflict in every case except two authors editing the same range.
package connector

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

// Magic is the first token of every connector file. The format version that
// follows it is the connector encoding's own version and is independent of the
// saga format version.
const (
	Magic         = "saga-connectors"
	FormatVersion = 1
	// Extension is the connector file suffix. It is intentionally not `.json`
	// so a v2 reader scanning `___diffs` for `*.json` cannot mistake one for an
	// evidence record.
	Extension = ".connectors"
	// EvidenceDirectory is the reserved metadata directory connector files live
	// in. It is deliberately the same `___diffs` directory v2 evidence uses: a
	// v2 loader only reads `*.json` there and skips every other entry, so a
	// saga carrying both encodings still loads, validates, and reports the same
	// coverage under an unmodified reader.
	EvidenceDirectory = "___diffs"
)

// Comparison is the hoisted diff identity: everything a `saga-diff://` URI
// repeats on every single atom.
type Comparison struct {
	Repository string
	Base       string
	Head       string
}

func (c Comparison) key() string { return c.Repository + "\x00" + c.Base + "\x00" + c.Head }

// alias is content addressed rather than positional. A positional alias would
// renumber every record line the moment an author inserted a note ahead of an
// existing one, turning a one-line addition into a whole-file rewrite and a
// guaranteed merge conflict.
func (c Comparison) alias() string { return shortHash(c.key()) }

// Record is one evidence atom or one contiguous run of them. A Lines record is
// dense by construction: every integer in [Start,End] was an owned atom when
// the record was written, which is what makes range coalescing reversible.
type Record struct {
	Comparison Comparison
	Note       string

	Kind string // "lines" or "event"

	Side  string // lines only: "old" or "new"
	Start int    // lines only
	End   int    // lines only

	Event string // event only

	// Path is the source path a record selects. An event record writes it, so
	// that a rename and a non-rename event read the same way. A line record
	// does not: its path is always the file's `source` header, and the parser
	// fills the field back in so a Record is usable on its own.
	Path    string
	OldPath string // rename only
	NewPath string // rename only
}

// File is one connector shard: the evidence one narrative target owns for one
// source path.
type File struct {
	// Owner is the stable target URN of the narrative element that owns this
	// evidence. The containing directory is authoritative; this field makes the
	// file self describing and lets a merge that moved a file be detected.
	Owner string
	// Source is the source path this shard covers. Renames are sharded under
	// the new path.
	Source  string
	Records []Record
}

func shortHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:3])
}

func noteAlias(note string) string { return shortHash("note\x00" + note) }

// SortRecords puts a file's records in canonical order. Ordering is a pure
// function of record content so two engines, and two sides of a merge, always
// agree on where a record belongs.
func SortRecords(records []Record) {
	sort.SliceStable(records, func(i, j int) bool { return recordLess(records[i], records[j]) })
}

func recordLess(a, b Record) bool {
	if a.Comparison.alias() != b.Comparison.alias() {
		return a.Comparison.alias() < b.Comparison.alias()
	}
	// Events sort ahead of lines: a file's lifecycle reads before its contents.
	if a.Kind != b.Kind {
		return a.Kind == "event"
	}
	if a.Kind == "event" {
		if a.Event != b.Event {
			return a.Event < b.Event
		}
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		if a.OldPath != b.OldPath {
			return a.OldPath < b.OldPath
		}
		if a.NewPath != b.NewPath {
			return a.NewPath < b.NewPath
		}
		return a.Note < b.Note
	}
	if a.Side != b.Side {
		// "new" before "old" so the side a reviewer reads first comes first.
		return a.Side == "new"
	}
	if a.Start != b.Start {
		return a.Start < b.Start
	}
	if a.End != b.End {
		return a.End < b.End
	}
	return a.Note < b.Note
}

// Write encodes a file in canonical form. The same File always produces the
// same bytes.
func Write(w io.Writer, file File) error {
	_, _, err := WriteSized(w, file)
	return err
}

// WriteSized behaves like Write and additionally reports how many of the bytes
// it wrote were the hoisted header. The header is the price of a shard that
// can be read on its own; knowing it is what says whether hoisting further —
// into a saga-wide comparison table — would buy anything worth the loss of
// self-containment.
func WriteSized(w io.Writer, file File) (header int64, total int64, err error) {
	records := append([]Record(nil), file.Records...)
	SortRecords(records)

	comparisons := map[string]Comparison{}
	notes := map[string]string{}
	for _, record := range records {
		comparisons[record.Comparison.alias()] = record.Comparison
		if record.Note != "" {
			notes[noteAlias(record.Note)] = record.Note
		}
	}

	counter := &countingWriter{Writer: w}
	out := bufio.NewWriter(counter)
	fmt.Fprintf(out, "%s %d\n", Magic, FormatVersion)
	fmt.Fprintf(out, "owner %s\n", encodeLine(file.Owner))
	fmt.Fprintf(out, "source %s\n", encodeLine(file.Source))

	for _, alias := range sortedKeys(comparisons) {
		comparison := comparisons[alias]
		fmt.Fprintf(out, "\ncomparison %s\n", alias)
		fmt.Fprintf(out, "  repository %s\n", encodeLine(comparison.Repository))
		fmt.Fprintf(out, "  base %s\n", encodeLine(comparison.Base))
		fmt.Fprintf(out, "  head %s\n", encodeLine(comparison.Head))
	}
	for _, alias := range sortedKeys(notes) {
		fmt.Fprintf(out, "\nnote %s\n", alias)
		for _, line := range strings.Split(notes[alias], "\n") {
			fmt.Fprintf(out, "  %s\n", encodeLine(line))
		}
	}
	if len(records) > 0 {
		fmt.Fprint(out, "\n")
	}
	if err := out.Flush(); err != nil {
		return 0, counter.total, err
	}
	header = counter.total

	for _, record := range records {
		note := "-"
		if record.Note != "" {
			note = noteAlias(record.Note)
		}
		switch {
		case record.Kind == "event" && record.Event == "rename":
			fmt.Fprintf(out, "%s %s event rename %s %s\n", record.Comparison.alias(), note,
				encodeToken(record.OldPath), encodeToken(record.NewPath))
		case record.Kind == "event":
			fmt.Fprintf(out, "%s %s event %s %s\n", record.Comparison.alias(), note, record.Event, encodeToken(record.Path))
		default:
			fmt.Fprintf(out, "%s %s lines %s %s\n", record.Comparison.alias(), note, record.Side, encodeRange(record.Start, record.End))
		}
	}
	if err := out.Flush(); err != nil {
		return header, counter.total, err
	}
	return header, counter.total, nil
}

type countingWriter struct {
	io.Writer
	total int64
}

func (w *countingWriter) Write(p []byte) (int, error) {
	n, err := w.Writer.Write(p)
	w.total += int64(n)
	return n, err
}

func encodeRange(start, end int) string {
	if start == end {
		return strconv.Itoa(start)
	}
	return strconv.Itoa(start) + "-" + strconv.Itoa(end)
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// encodeLine escapes a value that occupies the rest of a directive line. Only
// the two bytes that would break line framing need escaping, so paths and
// prose stay readable.
func encodeLine(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, "\n", `\n`, "\r", `\r`)
	return replacer.Replace(value)
}

// encodeToken escapes a value that shares a line with other fields. A space is
// escaped as `\_` because a space is a legal byte in a Git path and would
// otherwise split one path into two fields.
func encodeToken(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, "\n", `\n`, "\r", `\r`, " ", `\_`, "\t", `\t`)
	return replacer.Replace(value)
}

func decodeEscapes(value string) (string, error) {
	if !strings.Contains(value, `\`) {
		return value, nil
	}
	var out strings.Builder
	for i := 0; i < len(value); i++ {
		if value[i] != '\\' {
			out.WriteByte(value[i])
			continue
		}
		i++
		if i >= len(value) {
			return "", fmt.Errorf("dangling escape")
		}
		switch value[i] {
		case '\\':
			out.WriteByte('\\')
		case 'n':
			out.WriteByte('\n')
		case 'r':
			out.WriteByte('\r')
		case 't':
			out.WriteByte('\t')
		case '_':
			out.WriteByte(' ')
		default:
			return "", fmt.Errorf("unknown escape %q", `\`+string(value[i]))
		}
	}
	return out.String(), nil
}
