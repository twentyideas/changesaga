// Package testfixture builds deterministic on-disk fixtures shared by tests
// and benchmarks. It is internal so fixture helpers do not become public API.
package testfixture

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/twentyideas/changesaga/internal/diffuri"
	"github.com/twentyideas/changesaga/internal/gitdiff"
	"github.com/twentyideas/changesaga/internal/saga"
)

const largeSagaRepository = "https://example.test/bench/large-saga.git"

const coverageRangeWidth = 4

// LargeSagaOptions controls the scale of a generated source comparison and
// saga. Zero-valued fields are rejected so benchmarks cannot silently shrink.
type LargeSagaOptions struct {
	Chapters            int
	SectionsPerChapter  int
	FragmentsPerSection int
	SourceFiles         int
	ChangedLinesPerFile int
	ReviewsPerFragment  int
	Threads             int
	DiffReviews         int

	// CoverageRangeWidth is how many consecutive changed lines one diff
	// reference covers. Zero selects coverageRangeWidth. One reproduces the
	// shape `cover --changed-lines` writes today: a separate reference per
	// changed line, which is what sagas already on disk contain.
	CoverageRangeWidth int

	// CoverageTargets is how many fragments receive evidence. Zero spreads it
	// across every generated fragment, which gives each target a handful of
	// references. A small number reproduces a real saga, where a few narrative
	// targets own thousands of the comparison's atoms.
	CoverageTargets int
}

// DefaultLargeSagaOptions is intentionally large enough to catch nonlinear
// behavior while remaining practical for local benchmark iteration.
func DefaultLargeSagaOptions() LargeSagaOptions {
	return LargeSagaOptions{
		Chapters:            8,
		SectionsPerChapter:  6,
		FragmentsPerSection: 3,
		SourceFiles:         32,
		ChangedLinesPerFile: 64,
		ReviewsPerFragment:  2,
		Threads:             48,
		DiffReviews:         32,
	}
}

// LargeSaga describes the generated fixture and its exact scale.
type LargeSaga struct {
	Root        string
	Repository  string
	Base        string
	Head        string
	Chapters    int
	Sections    int
	Fragments   int
	Markdown    int
	SVG         int
	HTML        int
	Atoms       int
	Mappings    int
	References  int
	DiffFiles   int
	Reviews     int
	Threads     int
	DiffReviews int

	// CoverageRangeWidth and CoverageTargets are the resolved shape, with
	// zero-valued options replaced by the defaults they select.
	CoverageRangeWidth int
	CoverageTargets    int

	// MaxTargetReferences is the largest number of diff references a single
	// target owns. Selector resolution scans one target's references once per
	// atom that target owns, so this is what decides whether a fixture
	// exercises that scan at a realistic length.
	MaxTargetReferences int
}

type generatedFragment struct {
	dir    string
	target string
}

// GenerateLargeSaga creates a source Git repository and a fully covered saga
// beneath parent. Given the same options, the saga bytes and Git object IDs are
// stable across runs.
func GenerateLargeSaga(ctx context.Context, parent string, options LargeSagaOptions) (LargeSaga, error) {
	if err := validateLargeSagaOptions(options); err != nil {
		return LargeSaga{}, err
	}
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return LargeSaga{}, err
	}
	repositoryDir := filepath.Join(parent, "source")
	root := filepath.Join(parent, "large.saga")
	for _, path := range []string{repositoryDir, root} {
		if _, err := os.Lstat(path); err == nil {
			return LargeSaga{}, fmt.Errorf("generate large saga: %s already exists", path)
		} else if !os.IsNotExist(err) {
			return LargeSaga{}, err
		}
	}
	if err := createSourceRepository(ctx, repositoryDir, options); err != nil {
		return LargeSaga{}, err
	}
	base, err := gitOutput(ctx, repositoryDir, "rev-parse", "HEAD~1")
	if err != nil {
		return LargeSaga{}, err
	}
	changes, err := gitdiff.Read(ctx, repositoryDir, largeSagaRepository, base, "HEAD")
	if err != nil {
		return LargeSaga{}, fmt.Errorf("read generated source comparison: %w", err)
	}

	fixture := LargeSaga{
		Root: root, Repository: repositoryDir, Base: base, Head: "HEAD",
		Chapters: options.Chapters,
		Sections: options.Chapters * options.SectionsPerChapter,
		Atoms:    len(changes.Atoms),
	}
	fragments, err := createSagaTree(root, base, options, &fixture)
	if err != nil {
		return LargeSaga{}, err
	}
	shape, err := writeCoverageMappings(fragments, changes.Atoms, options)
	if err != nil {
		return LargeSaga{}, err
	}
	fixture.Mappings = shape.mappings
	fixture.References = shape.references
	fixture.DiffFiles = shape.diffFiles
	fixture.CoverageRangeWidth = resolveRangeWidth(options)
	fixture.CoverageTargets = shape.diffFiles
	fixture.MaxTargetReferences = shape.maxTargetReferences
	if err := writeThreads(root, fragments, options.Threads); err != nil {
		return LargeSaga{}, err
	}
	fixture.Threads = options.Threads
	if err := writeDiffReviews(root, changes, options.DiffReviews); err != nil {
		return LargeSaga{}, err
	}
	fixture.DiffReviews = options.DiffReviews
	return fixture, nil
}

func validateLargeSagaOptions(options LargeSagaOptions) error {
	values := []struct {
		name  string
		value int
	}{
		{"chapters", options.Chapters},
		{"sections per chapter", options.SectionsPerChapter},
		{"fragments per section", options.FragmentsPerSection},
		{"source files", options.SourceFiles},
		{"changed lines per file", options.ChangedLinesPerFile},
	}
	for _, value := range values {
		if value.value < 1 {
			return fmt.Errorf("large saga %s must be positive", value.name)
		}
	}
	for _, value := range []struct {
		name  string
		value int
	}{
		{"reviews per fragment", options.ReviewsPerFragment},
		{"threads", options.Threads},
		{"diff reviews", options.DiffReviews},
	} {
		if value.value < 0 {
			return fmt.Errorf("large saga %s cannot be negative", value.name)
		}
	}
	if options.DiffReviews > options.SourceFiles {
		return fmt.Errorf("large saga diff reviews cannot exceed source files")
	}
	if options.CoverageRangeWidth < 0 {
		return fmt.Errorf("large saga coverage range width cannot be negative")
	}
	if options.CoverageTargets < 0 {
		return fmt.Errorf("large saga coverage targets cannot be negative")
	}
	if fragments := options.Chapters * options.SectionsPerChapter * options.FragmentsPerSection; options.CoverageTargets > fragments {
		return fmt.Errorf("large saga coverage targets cannot exceed the %d generated fragments", fragments)
	}
	return nil
}

func createSourceRepository(ctx context.Context, dir string, options LargeSagaOptions) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	commands := [][]string{
		{"init", "-b", "main"},
		{"config", "user.name", "Fixture Author"},
		{"config", "user.email", "fixture@example.test"},
		{"remote", "add", "origin", largeSagaRepository},
	}
	for _, args := range commands {
		if _, err := gitOutput(ctx, dir, args...); err != nil {
			return err
		}
	}
	for file := 0; file < options.SourceFiles; file++ {
		if err := writeFile(filepath.Join(dir, sourcePath(file)), sourceContent("old", file, options.ChangedLinesPerFile)); err != nil {
			return err
		}
	}
	if _, err := gitOutput(ctx, dir, "add", "."); err != nil {
		return err
	}
	if _, err := gitOutput(ctx, dir, "-c", "commit.gpgSign=false", "commit", "-m", "fixture base"); err != nil {
		return err
	}
	for file := 0; file < options.SourceFiles; file++ {
		if err := writeFile(filepath.Join(dir, sourcePath(file)), sourceContent("new", file, options.ChangedLinesPerFile)); err != nil {
			return err
		}
	}
	if _, err := gitOutput(ctx, dir, "add", "."); err != nil {
		return err
	}
	_, err := gitOutput(ctx, dir, "-c", "commit.gpgSign=false", "commit", "-m", "fixture head")
	return err
}

func sourcePath(index int) string {
	return filepath.Join("src", fmt.Sprintf("component-%03d.txt", index))
}

func sourceContent(state string, file, lines int) string {
	var content strings.Builder
	for line := 1; line <= lines; line++ {
		fmt.Fprintf(&content, "%s component %03d line %04d deterministic benchmark content\n", state, file, line)
	}
	return content.String()
}

func createSagaTree(root, base string, options LargeSagaOptions, fixture *LargeSaga) ([]generatedFragment, error) {
	manifest := saga.Manifest{
		Schema: saga.SchemaURL, Version: saga.CurrentVersion, ID: "large-benchmark", Title: "Large benchmark saga",
		Source: saga.Source{Repository: largeSagaRepository, Base: base, Head: "HEAD"},
	}
	if err := writeJSON(filepath.Join(root, "saga.json"), manifest); err != nil {
		return nil, err
	}
	if err := writeFragment(filepath.Join(root, "overview.fragment"), "overview", "Overview", "text/markdown", "content.md", "# Large benchmark saga {#overview}\n\nDeterministic fixture overview.\n", "overview"); err != nil {
		return nil, err
	}
	fixture.Fragments++
	fixture.Markdown++

	fragments := make([]generatedFragment, 0, options.Chapters*options.SectionsPerChapter*options.FragmentsPerSection)
	for chapter := 0; chapter < options.Chapters; chapter++ {
		chapterName := fmt.Sprintf("chapter-%02d.chapter", chapter)
		chapterDir := filepath.Join(root, chapterName)
		chapterID := fmt.Sprintf("chapter-%02d", chapter)
		chapterManifest := saga.ChapterManifest{Version: saga.CurrentVersion, ID: chapterID, Title: fmt.Sprintf("Chapter %02d", chapter), Order: chapter + 1}
		if err := writeJSON(filepath.Join(chapterDir, "chapter.json"), chapterManifest); err != nil {
			return nil, err
		}
		for section := 0; section < options.SectionsPerChapter; section++ {
			sectionDir := filepath.Join(chapterDir, fmt.Sprintf("section-%02d", section))
			sectionID := fmt.Sprintf("c%02d-section-%02d", chapter, section)
			sectionManifest := saga.SectionManifest{Version: saga.CurrentVersion, ID: sectionID, Title: fmt.Sprintf("Section %02d.%02d", chapter, section), Order: section + 1}
			if err := writeJSON(filepath.Join(sectionDir, "section.json"), sectionManifest); err != nil {
				return nil, err
			}
			for fragment := 0; fragment < options.FragmentsPerSection; fragment++ {
				fragmentID := fmt.Sprintf("c%02d-s%02d-f%02d", chapter, section, fragment)
				fragmentDir := filepath.Join(sectionDir, fmt.Sprintf("fragment-%02d.fragment", fragment))
				mediaType, entrypoint, content, landmarkID := fragmentAsset(fragmentID, fragment)
				if err := writeFragment(fragmentDir, fragmentID, fmt.Sprintf("Fragment %02d.%02d.%02d", chapter, section, fragment), mediaType, entrypoint, content, landmarkID); err != nil {
					return nil, err
				}
				switch mediaType {
				case "text/markdown":
					fixture.Markdown++
				case "image/svg+xml":
					fixture.SVG++
				case "text/html":
					fixture.HTML++
				}
				for review := 0; review < options.ReviewsPerFragment; review++ {
					if err := writeApproval(fragmentDir, fragmentID, review); err != nil {
						return nil, err
					}
					fixture.Reviews++
				}
				fragments = append(fragments, generatedFragment{dir: fragmentDir, target: saga.FragmentTarget("large-benchmark", fragmentID)})
				fixture.Fragments++
			}
		}
	}
	return fragments, nil
}

func fragmentAsset(id string, index int) (mediaType, entrypoint, content, landmarkID string) {
	landmarkID = "focus-" + id
	switch index % 3 {
	case 0:
		return "text/markdown", "content.md", fmt.Sprintf("# Deterministic narrative {#%s}\n\nThis Markdown fragment explains %s and its review boundary.\n", landmarkID, id), landmarkID
	case 1:
		return "image/svg+xml", "diagram.svg", fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 800 450"><title>%s</title><g id="%s"><rect x="40" y="40" width="720" height="370" rx="24" fill="#e8eef8"/><path d="M120 225H680" stroke="#335c9b" stroke-width="12"/></g></svg>`+"\n", id, landmarkID), landmarkID
	default:
		return "text/html", "index.html", fmt.Sprintf(`<!doctype html><html><head><meta charset="utf-8"><title>%s</title></head><body><main id="%s"><h1>Interactive fixture</h1><button type="button">Advance</button></main></body></html>`+"\n", id, landmarkID), landmarkID
	}
}

func writeFragment(dir, id, title, mediaType, entrypoint, content, landmarkID string) error {
	manifest := saga.FragmentManifest{Version: saga.CurrentVersion, ID: id, Title: title, MediaType: mediaType, Entrypoint: entrypoint}
	if err := writeJSON(filepath.Join(dir, "fragment.json"), manifest); err != nil {
		return err
	}
	if err := writeFile(filepath.Join(dir, filepath.FromSlash(entrypoint)), content); err != nil {
		return err
	}
	selector := saga.LandmarkSelector{Type: "element", ElementID: landmarkID}
	if mediaType == "text/markdown" {
		selector = saga.LandmarkSelector{Type: "heading", HeadingID: landmarkID}
	}
	landmark := saga.Landmark{Version: saga.CurrentVersion, ID: landmarkID, Label: "Review focus", Selector: selector}
	return writeJSON(filepath.Join(dir, "___landmarks", landmarkID+".landmark", "landmark.json"), landmark)
}

func writeApproval(fragmentDir, fragmentID string, index int) error {
	state := "approved"
	if index%2 == 0 {
		state = "open"
	}
	review := saga.Review{
		Version: saga.CurrentVersion, ID: fmt.Sprintf("review-%s-%02d", fragmentID, index), Author: "Benchmark Reviewer",
		State: state, Body: "Deterministic approval history.", CreatedAt: fixtureTime(index),
	}
	return writeJSON(filepath.Join(fragmentDir, "___approvals", fmt.Sprintf("review-%02d.json", index)), review)
}

type rangedMapping struct {
	reference saga.DiffReference
	atoms     int
}

type coverageShape struct {
	mappings            int
	references          int
	diffFiles           int
	maxTargetReferences int
}

func writeCoverageMappings(fragments []generatedFragment, atoms []gitdiff.Atom, options LargeSagaOptions) (coverageShape, error) {
	ranges, err := coverageRanges(atoms, resolveRangeWidth(options))
	if err != nil {
		return coverageShape{}, err
	}
	targets := options.CoverageTargets
	if targets < 1 || targets > len(fragments) {
		targets = len(fragments)
	}
	shape := coverageShape{references: len(ranges)}
	grouped := make([][]saga.DiffReference, len(fragments))
	for index, mapping := range ranges {
		target := index % targets
		grouped[target] = append(grouped[target], mapping.reference)
		shape.mappings += mapping.atoms
	}
	for index, fragment := range fragments {
		if len(grouped[index]) == 0 {
			continue
		}
		value := saga.DiffFile{Version: saga.CurrentVersion, Diffs: grouped[index]}
		if err := writeJSON(filepath.Join(fragment.dir, "___diffs", "coverage.json"), value); err != nil {
			return coverageShape{}, err
		}
		shape.diffFiles++
		if len(grouped[index]) > shape.maxTargetReferences {
			shape.maxTargetReferences = len(grouped[index])
		}
	}
	return shape, nil
}

func resolveRangeWidth(options LargeSagaOptions) int {
	if options.CoverageRangeWidth < 1 {
		return coverageRangeWidth
	}
	return options.CoverageRangeWidth
}

func coverageRanges(atoms []gitdiff.Atom, width int) ([]rangedMapping, error) {
	result := make([]rangedMapping, 0, (len(atoms)+width-1)/width)
	for start := 0; start < len(atoms); {
		first, err := diffuri.Parse(atoms[start].URI)
		if err != nil {
			return nil, err
		}
		end := start + 1
		for end < len(atoms) && end-start < width {
			next, err := diffuri.Parse(atoms[end].URI)
			if err != nil {
				return nil, err
			}
			if !consecutiveLine(first, next, end-start) {
				break
			}
			end++
		}
		uri := atoms[start].URI
		if first.Kind == "line" && end-start > 1 {
			first.End = first.Start + end - start - 1
			uri, err = diffuri.Build(first)
			if err != nil {
				return nil, err
			}
		}
		result = append(result, rangedMapping{
			reference: saga.DiffReference{URI: uri, Note: fmt.Sprintf("atoms-%05d-%05d", start, end-1)},
			atoms:     end - start,
		})
		start = end
	}
	return result, nil
}

func consecutiveLine(first, next diffuri.Reference, offset int) bool {
	return first.Kind == "line" && next.Kind == "line" &&
		first.Repository == next.Repository && first.Base == next.Base && first.Head == next.Head &&
		first.Path == next.Path && first.Side == next.Side && next.Start == first.Start+offset && next.End == next.Start
}

func writeThreads(root string, fragments []generatedFragment, count int) error {
	for index := 0; index < count; index++ {
		threadID := fmt.Sprintf("thread-%03d", index)
		messageID := fmt.Sprintf("message-%03d", index)
		threadDir := filepath.Join(root, "___review", "threads", threadID+".thread")
		manifest := saga.ThreadManifest{
			Version: saga.CurrentVersion, ID: threadID, Target: fragments[index%len(fragments)].target,
			Anchor: saga.Anchor{Type: "target"}, Kind: "comment", CreatedBy: "Benchmark Reviewer", CreatedAt: fixtureTime(index),
		}
		if err := writeJSON(filepath.Join(threadDir, "thread.json"), manifest); err != nil {
			return err
		}
		messageDir := filepath.Join(threadDir, "messages", messageID+".message")
		message := saga.MessageManifest{Version: saga.CurrentVersion, ID: messageID, Author: "Benchmark Reviewer", CreatedAt: fixtureTime(index)}
		if err := writeJSON(filepath.Join(messageDir, "message.json"), message); err != nil {
			return err
		}
		bodyID := fmt.Sprintf("thread-body-%03d", index)
		landmarkID := fmt.Sprintf("comment-focus-%03d", index)
		body := fmt.Sprintf("# Review comment {#%s}\n\nReview comment %03d.\n", landmarkID, index)
		if err := writeFragment(filepath.Join(messageDir, "body.fragment"), bodyID, "Review comment", "text/markdown", "content.md", body, landmarkID); err != nil {
			return err
		}
		event := saga.ThreadEvent{Version: saga.CurrentVersion, ID: fmt.Sprintf("event-%03d", index), Author: "Benchmark Reviewer", State: "resolved", CreatedAt: fixtureTime(index).Add(time.Second)}
		if err := writeJSON(filepath.Join(threadDir, "events", fmt.Sprintf("event-%03d.json", index)), event); err != nil {
			return err
		}
	}
	return nil
}

func writeDiffReviews(root string, changes gitdiff.ChangeSet, count int) error {
	for index := 0; index < count; index++ {
		uri, err := diffuri.Build(diffuri.Reference{
			Repository: changes.Repository, Base: changes.BaseOID, Head: changes.HeadOID,
			Kind: "file", Path: filepath.ToSlash(sourcePath(index)),
		})
		if err != nil {
			return err
		}
		review := saga.DiffReview{
			Version: saga.CurrentVersion, ID: fmt.Sprintf("diff-review-%03d", index), URI: uri,
			Author: "Benchmark Reviewer", State: "reviewed", CreatedAt: fixtureTime(index),
		}
		if err := writeJSON(filepath.Join(root, "___review", "diffs", fmt.Sprintf("review-%03d.json", index)), review); err != nil {
			return err
		}
	}
	return nil
}

func fixtureTime(index int) time.Time {
	return time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC).Add(time.Duration(index) * time.Minute)
}

func writeJSON(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func writeFile(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

func gitOutput(ctx context.Context, dir string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, "git", args...)
	command.Dir = dir
	command.Env = append(os.Environ(),
		"GIT_AUTHOR_DATE=2026-01-02T03:04:05Z",
		"GIT_COMMITTER_DATE=2026-01-02T03:04:05Z",
	)
	output, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}
