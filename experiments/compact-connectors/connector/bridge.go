package connector

import (
	"fmt"
	"sort"

	"github.com/twentyideas/changesaga/internal/diffuri"
	"github.com/twentyideas/changesaga/internal/saga"
)

// Granularity selects how many source atoms one line record may describe.
type Granularity int

const (
	// Exact writes one record per changed line. Selector identity, and
	// therefore stale-reference reporting, is bit-for-bit what v2 produced.
	Exact Granularity = iota
	// Ranges coalesces runs of consecutive line numbers that share an owner, a
	// comparison, a side, and a note. A coalesced record is dense by
	// construction, so Expand recovers the exact atom set.
	Ranges
)

// FromReference converts one v2 evidence reference into a record.
func FromReference(reference saga.DiffReference) (Record, error) {
	parsed, err := diffuri.Parse(reference.URI)
	if err != nil {
		return Record{}, err
	}
	comparison := Comparison{Repository: parsed.Repository, Base: parsed.Base, Head: parsed.Head}
	switch parsed.Kind {
	case "line":
		return Record{
			Comparison: comparison, Note: reference.Note, Kind: "lines",
			Side: parsed.Side, Start: parsed.Start, End: parsed.End, Path: parsed.Path,
		}, nil
	case "event":
		return Record{
			Comparison: comparison, Note: reference.Note, Kind: "event",
			Event: parsed.Event, Path: parsed.Path, OldPath: parsed.OldPath, NewPath: parsed.NewPath,
		}, nil
	default:
		// `/file` URIs are review-progress targets, never coverage links.
		return Record{}, fmt.Errorf("coverage links must address lines or events, not %s", parsed.Kind)
	}
}

// SourcePath is the shard key for a record: the source file whose changes the
// record selects. A rename shards under its new path so a file's rename event
// and its changed lines stay in one shard.
func (r Record) SourcePath() string {
	if r.Kind == "event" {
		if r.Event == "rename" {
			return r.NewPath
		}
		return r.Path
	}
	return r.Path
}

// References rebuilds v2 evidence references from a file's records. A ranged
// line record becomes one ranged reference at Ranges granularity and one
// reference per line at Exact granularity; because ranges are dense the two
// select exactly the same atoms.
func (f File) References(granularity Granularity) ([]saga.DiffReference, error) {
	records := append([]Record(nil), f.Records...)
	SortRecords(records)
	var references []saga.DiffReference
	for _, record := range records {
		built, err := record.references(f.Source, granularity)
		if err != nil {
			return nil, err
		}
		references = append(references, built...)
	}
	return references, nil
}

func (r Record) references(source string, granularity Granularity) ([]saga.DiffReference, error) {
	if r.Kind == "event" {
		uri, err := diffuri.Build(diffuri.Reference{
			Repository: r.Comparison.Repository, Base: r.Comparison.Base, Head: r.Comparison.Head,
			Kind: "event", Event: r.Event, Path: r.Path, OldPath: r.OldPath, NewPath: r.NewPath,
		})
		if err != nil {
			return nil, err
		}
		return []saga.DiffReference{{URI: uri, Note: r.Note}}, nil
	}
	build := func(start, end int) (saga.DiffReference, error) {
		uri, err := diffuri.Build(diffuri.Reference{
			Repository: r.Comparison.Repository, Base: r.Comparison.Base, Head: r.Comparison.Head,
			Kind: "line", Path: source, Side: r.Side, Start: start, End: end,
		})
		return saga.DiffReference{URI: uri, Note: r.Note}, err
	}
	if granularity == Ranges {
		reference, err := build(r.Start, r.End)
		if err != nil {
			return nil, err
		}
		return []saga.DiffReference{reference}, nil
	}
	references := make([]saga.DiffReference, 0, r.End-r.Start+1)
	for line := r.Start; line <= r.End; line++ {
		reference, err := build(line, line)
		if err != nil {
			return nil, err
		}
		references = append(references, reference)
	}
	return references, nil
}

// Coalesce merges runs of consecutive single-line records that agree on
// comparison, side, and note. Only consecutive integers are merged, so every
// integer inside a produced range was an owned atom and no line acquires an
// owner it did not already have.
func Coalesce(records []Record) []Record {
	lines := map[string][]Record{}
	var events []Record
	for _, record := range records {
		if record.Kind == "event" {
			events = append(events, record)
			continue
		}
		key := record.Comparison.key() + "\x00" + record.Side + "\x00" + record.Note
		lines[key] = append(lines[key], record)
	}
	out := events
	for _, key := range sortedKeys(lines) {
		group := lines[key]
		sort.Slice(group, func(i, j int) bool {
			if group[i].Start != group[j].Start {
				return group[i].Start < group[j].Start
			}
			return group[i].End < group[j].End
		})
		current := group[0]
		for _, next := range group[1:] {
			if next.Start <= current.End+1 {
				if next.End > current.End {
					current.End = next.End
				}
				continue
			}
			out = append(out, current)
			current = next
		}
		out = append(out, current)
	}
	SortRecords(out)
	return out
}
