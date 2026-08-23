package connector

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Parse decodes a connector file. It is strict on purpose: an unknown
// directive, an unresolved alias, or a non canonical range is an error rather
// than a silently dropped atom, because a dropped atom would quietly turn an
// incomplete saga into a complete looking one.
func Parse(r io.Reader) (File, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	var file File
	comparisons := map[string]Comparison{}
	notes := map[string][]string{}

	var currentComparison string
	var currentNote string
	sawMagic := false
	line := 0

	fail := func(format string, args ...any) (File, error) {
		return File{}, fmt.Errorf("line %d: %s", line, fmt.Sprintf(format, args...))
	}

	for scanner.Scan() {
		line++
		raw := scanner.Text()
		if strings.HasPrefix(strings.TrimSpace(raw), "#") {
			continue
		}
		if strings.TrimSpace(raw) == "" {
			continue
		}

		// An indented line continues the block it follows.
		if raw[0] == ' ' || raw[0] == '\t' {
			body := strings.TrimLeft(raw, " \t")
			switch {
			case currentComparison != "":
				key, value, ok := strings.Cut(body, " ")
				if !ok {
					return fail("comparison field %q needs a value", body)
				}
				decoded, err := decodeEscapes(value)
				if err != nil {
					return fail("%v", err)
				}
				comparison := comparisons[currentComparison]
				switch key {
				case "repository":
					comparison.Repository = decoded
				case "base":
					comparison.Base = decoded
				case "head":
					comparison.Head = decoded
				default:
					return fail("unknown comparison field %q", key)
				}
				comparisons[currentComparison] = comparison
			case currentNote != "":
				decoded, err := decodeEscapes(body)
				if err != nil {
					return fail("%v", err)
				}
				notes[currentNote] = append(notes[currentNote], decoded)
			default:
				return fail("indented line outside a block")
			}
			continue
		}

		currentComparison, currentNote = "", ""
		head, rest, _ := strings.Cut(raw, " ")
		switch head {
		case Magic:
			if sawMagic {
				return fail("duplicate %s header", Magic)
			}
			version, err := strconv.Atoi(strings.TrimSpace(rest))
			if err != nil || version != FormatVersion {
				return fail("unsupported connector format version %q; expected %d", strings.TrimSpace(rest), FormatVersion)
			}
			sawMagic = true
		case "owner":
			if !sawMagic {
				return fail("owner before the %s header", Magic)
			}
			decoded, err := decodeEscapes(rest)
			if err != nil {
				return fail("%v", err)
			}
			file.Owner = decoded
		case "source":
			decoded, err := decodeEscapes(rest)
			if err != nil {
				return fail("%v", err)
			}
			file.Source = decoded
		case "comparison":
			alias := strings.TrimSpace(rest)
			if alias == "" {
				return fail("comparison needs an alias")
			}
			if _, exists := comparisons[alias]; exists {
				return fail("duplicate comparison alias %q", alias)
			}
			comparisons[alias] = Comparison{}
			currentComparison = alias
		case "note":
			alias := strings.TrimSpace(rest)
			if alias == "" {
				return fail("note needs an alias")
			}
			if _, exists := notes[alias]; exists {
				return fail("duplicate note alias %q", alias)
			}
			notes[alias] = nil
			currentNote = alias
		default:
			record, err := parseRecord(raw, comparisons, notes)
			if err != nil {
				return fail("%v", err)
			}
			file.Records = append(file.Records, record)
		}
	}
	if err := scanner.Err(); err != nil {
		return File{}, err
	}
	if !sawMagic {
		return File{}, fmt.Errorf("missing %s header", Magic)
	}
	if file.Owner == "" {
		return File{}, fmt.Errorf("missing owner directive")
	}
	if file.Source == "" {
		return File{}, fmt.Errorf("missing source directive")
	}
	// A line record's path is the file's source header, which may be read
	// after the records; fill it in once the header is known.
	for i := range file.Records {
		if file.Records[i].Kind == "lines" {
			file.Records[i].Path = file.Source
		}
	}
	return file, nil
}

func parseRecord(raw string, comparisons map[string]Comparison, notes map[string][]string) (Record, error) {
	fields := strings.Fields(raw)
	if len(fields) < 4 {
		return Record{}, fmt.Errorf("record needs at least a comparison, a note, a kind, and a value")
	}
	comparison, ok := comparisons[fields[0]]
	if !ok {
		return Record{}, fmt.Errorf("record references undeclared comparison %q", fields[0])
	}
	record := Record{Comparison: comparison}
	if fields[1] != "-" {
		lines, ok := notes[fields[1]]
		if !ok {
			return Record{}, fmt.Errorf("record references undeclared note %q", fields[1])
		}
		record.Note = strings.Join(lines, "\n")
	}
	switch fields[2] {
	case "event":
		record.Kind = "event"
		record.Event = fields[3]
		if record.Event == "rename" {
			if len(fields) != 6 {
				return Record{}, fmt.Errorf("rename records carry exactly an old and a new path")
			}
			oldPath, err := decodeEscapes(fields[4])
			if err != nil {
				return Record{}, err
			}
			newPath, err := decodeEscapes(fields[5])
			if err != nil {
				return Record{}, err
			}
			record.OldPath, record.NewPath = oldPath, newPath
			return record, nil
		}
		if len(fields) != 5 {
			return Record{}, fmt.Errorf("event records carry exactly one path")
		}
		path, err := decodeEscapes(fields[4])
		if err != nil {
			return Record{}, err
		}
		record.Path = path
		return record, nil
	case "lines":
		record.Kind = "lines"
		if len(fields) != 5 {
			return Record{}, fmt.Errorf("line records carry exactly a side and a range")
		}
		record.Side = fields[3]
		if record.Side != "old" && record.Side != "new" {
			return Record{}, fmt.Errorf("line side must be old or new, not %q", record.Side)
		}
		start, end, err := parseRange(fields[4])
		if err != nil {
			return Record{}, err
		}
		record.Start, record.End = start, end
		return record, nil
	default:
		return Record{}, fmt.Errorf("unknown record kind %q", fields[2])
	}
}

func parseRange(value string) (int, int, error) {
	startText, endText, ranged := strings.Cut(value, "-")
	start, err := strconv.Atoi(startText)
	if err != nil {
		return 0, 0, fmt.Errorf("range start %q is not a number", startText)
	}
	if !ranged {
		if start < 1 {
			return 0, 0, fmt.Errorf("line numbers start at 1")
		}
		return start, start, nil
	}
	end, err := strconv.Atoi(endText)
	if err != nil {
		return 0, 0, fmt.Errorf("range end %q is not a number", endText)
	}
	if start < 1 || end < start {
		return 0, 0, fmt.Errorf("range %q is not ascending and one based", value)
	}
	if start == end {
		// A single line must be written as "5", never "5-5", so one atom has
		// exactly one canonical spelling.
		return 0, 0, fmt.Errorf("single line range %q must be written as %d", value, start)
	}
	return start, end, nil
}
