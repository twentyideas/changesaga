package cli

import (
	"context"
	"fmt"
	"html"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/twentyideas/changesaga/internal/saga"
	"github.com/twentyideas/changesaga/internal/store"
)

func requireSlideSaga(document *saga.Saga, command string) error {
	if document.Manifest.Version != saga.SlideSagaVersion || document.Manifest.Presentation == nil || document.Manifest.Presentation.Mode != "slides" {
		return fmt.Errorf("%s requires a v4 slide-native Saga; report Sagas are not silently paginated", command)
	}
	return nil
}

func AddDeck(_ context.Context, args []string, out io.Writer) error {
	flags := commandFlags("add-deck", commandUsage["add-deck"], out)
	id := flags.String("id", "", "stable deck identifier")
	title := flags.String("title", "", "deck title")
	role := flags.String("role", "change", "overview or change")
	var rank optionalInt
	flags.Var(&rank, "rank", "non-negative review order; defaults after the last deck")
	objective := flags.String("objective", "", "one concise reviewer objective")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 2 {
		return fmt.Errorf("usage: %s", commandUsage["add-deck"])
	}
	name := store.Slug(strings.TrimSuffix(flags.Arg(1), ".deck"))
	if *id == "" {
		*id = name
	}
	if *title == "" {
		*title = strings.ReplaceAll(name, "-", " ")
	}
	if *objective == "" {
		return fmt.Errorf("--objective is required")
	}
	if (rank.set && rank.value < 0) || utf8.RuneCountInString(*objective) > 240 {
		return fmt.Errorf("--rank must be non-negative and --objective cannot exceed 240 characters")
	}
	var created, target string
	err := authorMutation(flags.Arg(0), func(document *saga.Saga) error {
		if err := requireSlideSaga(document, "add-deck"); err != nil {
			return err
		}
		if !saga.ValidID(*id) || targetIDExists(document, *id) {
			return fmt.Errorf("deck id %q is invalid or already used", *id)
		}
		if *role != "change" {
			return fmt.Errorf("v4 init creates the single overview deck; additional decks must use --role change")
		}
		chosenRank := rank.value
		if !rank.set {
			for _, deck := range document.Decks {
				if deck.Rank >= chosenRank {
					chosenRank = deck.Rank + 10
				}
			}
		}
		manifest := saga.DeckManifest{Version: saga.SlideSagaVersion, ID: *id, Title: *title, Role: *role, Rank: chosenRank, Objective: strings.TrimSpace(*objective)}
		target = saga.DeckTarget(document.Manifest.ID, *id)
		filename, err := saga.FlatDeckFilename(target, chosenRank)
		if err != nil {
			return err
		}
		path := filepath.Join(document.Root, filename)
		if err := store.WriteJSON(path, manifest, true); err != nil {
			return err
		}
		created = filename
		return nil
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Added deck %s\nTarget: %s\nNext: change-saga add-slide --deck %s --intent explain --layout diagram %s first-slide\n", filepath.ToSlash(created), target, *id, flags.Arg(0))
	return nil
}

func AddSlide(_ context.Context, args []string, out io.Writer) error {
	flags := commandFlags("add-slide", commandUsage["add-slide"], out)
	deckTarget := flags.String("deck", "", "containing deck path, id, or URN")
	id := flags.String("id", "", "stable slide identifier")
	title := flags.String("title", "", "slide title")
	intent := flags.String("intent", "", "reviewer job: orient, explain, compare, trace, prove, risk, or conclude")
	layout := flags.String("layout", "", "canvas arrangement, not diagram meaning: hero, diagram, before-after, sequence, evidence, risk, or custom")
	var rank optionalInt
	flags.Var(&rank, "rank", "non-negative review order; defaults after the last slide")
	takeaway := flags.String("takeaway", "", "single reviewer takeaway (maximum 180 characters)")
	rationale := flags.String("exception-rationale", "", "required reason for a custom layout")
	source := flags.String("source", "", "SVG, image, or self-contained HTML source")
	mediaType := flags.String("media-type", "image/svg+xml", "visual media type")
	entrypoint := flags.String("entrypoint", "slide.svg", "simple filename whose extension selects the compact slide asset name")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 2 || *deckTarget == "" || *intent == "" || *layout == "" {
		return fmt.Errorf("usage: %s", commandUsage["add-slide"])
	}
	name := store.Slug(strings.TrimSuffix(flags.Arg(1), ".slide"))
	if *id == "" {
		*id = name
	}
	if *title == "" {
		*title = strings.ReplaceAll(name, "-", " ")
	}
	if *takeaway == "" {
		*takeaway = *title
	}
	validIntent := map[string]bool{"orient": true, "explain": true, "compare": true, "trace": true, "prove": true, "risk": true, "conclude": true}
	validLayout := map[string]bool{"hero": true, "diagram": true, "before-after": true, "sequence": true, "evidence": true, "risk": true, "custom": true}
	validMedia := map[string]bool{"image/svg+xml": true, "image/png": true, "image/jpeg": true, "image/webp": true, "text/html": true}
	if (rank.set && rank.value < 0) || !validIntent[*intent] || !validLayout[*layout] || !validMedia[*mediaType] {
		return fmt.Errorf("unsupported intent, layout, media type, or negative rank")
	}
	if utf8.RuneCountInString(*title) > 100 || utf8.RuneCountInString(*takeaway) > 180 {
		return fmt.Errorf("slide title/takeaway exceed the 100/180 character density limits")
	}
	if *layout == "custom" && strings.TrimSpace(*rationale) == "" {
		return fmt.Errorf("--exception-rationale is required for --layout custom")
	}
	if reason := saga.EntrypointError(*entrypoint); reason != "" {
		return fmt.Errorf("%s", reason)
	}
	if strings.Contains(*entrypoint, "/") {
		return fmt.Errorf("v4 --entrypoint is a simple filename used only to select the slide asset extension; nested paths are refused")
	}
	var data []byte
	var err error
	if *source != "" {
		info, statErr := os.Stat(*source)
		if statErr != nil {
			return fmt.Errorf("read slide source: %w", statErr)
		}
		if info.IsDir() {
			return fmt.Errorf("v4 slides require one self-contained SVG, image, or HTML file; source directories are not portable")
		}
		data, err = os.ReadFile(*source)
		if err != nil {
			return fmt.Errorf("read slide source: %w", err)
		}
	} else if *mediaType == "image/svg+xml" {
		data = []byte(defaultSlideSVG(*title, *takeaway))
	} else {
		return fmt.Errorf("--source is required unless --media-type is image/svg+xml")
	}
	var created, target string
	err = authorMutation(flags.Arg(0), func(document *saga.Saga) error {
		if err := requireSlideSaga(document, "add-slide"); err != nil {
			return err
		}
		deck := findDeck(document, *deckTarget)
		if deck == nil {
			return fmt.Errorf("--deck must identify an existing deck")
		}
		if !saga.ValidID(*id) || targetIDExists(document, *id) {
			return fmt.Errorf("slide id %q is invalid or already used", *id)
		}
		chosenRank := rank.value
		if !rank.set {
			for _, slide := range deck.Slides {
				if slide.Rank >= chosenRank {
					chosenRank = slide.Rank + 10
				}
			}
		}
		target = saga.SlideTarget(document.Manifest.ID, *id)
		filename, err := saga.FlatSlideFilename(deck.Target, target, chosenRank)
		if err != nil {
			return err
		}
		extension := filepath.Ext(*entrypoint)
		assetName, err := saga.FlatSlideAssetFilename(filename, extension)
		if err != nil {
			return err
		}
		manifest := saga.SlideManifest{Version: saga.SlideSagaVersion, ID: *id, DeckID: deck.ID, Title: *title, Rank: chosenRank, Intent: *intent, Layout: *layout, MediaType: *mediaType, Entrypoint: assetName, Takeaway: *takeaway, ReadingOrder: []string{}, ExceptionRationale: *rationale}
		assetPath := filepath.Join(document.Root, assetName)
		manifestPath := filepath.Join(document.Root, filename)
		if err := store.WriteFile(assetPath, data, 0o644, true); err != nil {
			return err
		}
		if err := store.WriteJSON(manifestPath, manifest, true); err != nil {
			_ = os.Remove(assetPath)
			return err
		}
		created = filename
		return nil
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Added slide %s\nTarget: %s\nNext: change-saga set-slide-content --target %s --source FILE %s\n", filepath.ToSlash(created), target, target, flags.Arg(0))
	return nil
}

func AddItem(_ context.Context, args []string, out io.Writer) error {
	flags := commandFlags("add-item", commandUsage["add-item"], out)
	slideTarget := flags.String("slide", "", "containing slide path, id, or URN")
	id := flags.String("id", "", "stable lowercase item identifier")
	kind := flags.String("kind", "", "node, edge, region, transition, statement, risk, metric, example, or callout")
	label := flags.String("label", "", "concise reviewer-facing label")
	description := flags.String("description", "", "non-visual semantic description")
	elementID := flags.String("element-id", "", "id of an SVG or HTML element")
	region := flags.String("region", "", "normalized image region x,y,width,height")
	hotspot := flags.String("hotspot", "", "optional normalized on-canvas hit area")
	about := flags.String("about", "", "for callouts, another item id on this slide")
	body := flags.String("body", "", "required concise callout body")
	placement := flags.String("placement", "", "top, right, bottom, left, or overlay")
	leader := flags.String("leader", "", "none, line, or arrow")
	var rank optionalInt
	flags.Var(&rank, "rank", "non-negative item order; defaults after the last item")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 || *slideTarget == "" || *kind == "" {
		return fmt.Errorf("usage: %s", commandUsage["add-item"])
	}
	if (*elementID == "") == (*region == "") {
		return fmt.Errorf("provide exactly one selector: --element-id or --region")
	}
	var selector saga.LandmarkSelector
	if *elementID != "" {
		selector = saga.LandmarkSelector{Type: "element", ElementID: *elementID}
		if *id == "" {
			*id = *elementID
		}
	} else {
		parsed, err := parseLandmarkRegion(*region)
		if err != nil {
			return fmt.Errorf("invalid --region: %w", err)
		}
		selector = saga.LandmarkSelector{Type: "region", X: parsed.X, Y: parsed.Y, Width: parsed.Width, Height: parsed.Height}
		if *id == "" {
			*id = "region"
		}
	}
	if !saga.ValidMarkdownAnchor(*id) {
		return fmt.Errorf("--id must begin with a lowercase letter and contain only lowercase letters, digits, and hyphens")
	}
	if *label == "" {
		*label = strings.ReplaceAll(*id, "-", " ")
	}
	validKind := map[string]bool{"node": true, "edge": true, "region": true, "transition": true, "statement": true, "risk": true, "metric": true, "example": true, "callout": true}
	if !validKind[*kind] {
		return fmt.Errorf("unsupported --kind %q", *kind)
	}
	if utf8.RuneCountInString(*label) > 100 || utf8.RuneCountInString(*description) > 240 {
		return fmt.Errorf("item label/description exceed the 100/240 character density limits")
	}
	if *kind == "callout" {
		if strings.TrimSpace(*body) == "" || utf8.RuneCountInString(*body) > 240 {
			return fmt.Errorf("callout --body must contain 1 to 240 characters")
		}
	} else if *about != "" || *body != "" || *placement != "" || *leader != "" {
		return fmt.Errorf("--about, --body, --placement, and --leader require --kind callout")
	}
	var hotspotRegion *saga.LandmarkRegion
	if *hotspot != "" {
		parsed, err := parseLandmarkRegion(*hotspot)
		if err != nil {
			return fmt.Errorf("invalid --hotspot: %w", err)
		}
		hotspotRegion = &parsed
	}
	var created, target string
	err := authorMutation(flags.Arg(0), func(document *saga.Saga) error {
		if err := requireSlideSaga(document, "add-item"); err != nil {
			return err
		}
		slide := findSlide(document, *slideTarget)
		if slide == nil {
			return fmt.Errorf("--slide must identify an existing slide")
		}
		if len(slide.Items) >= 7 && slide.Layout != "custom" {
			return fmt.Errorf("standard layouts allow at most 7 semantic Items; split the slide")
		}
		aboutFound := *about == ""
		for _, existing := range slide.Items {
			if existing.ID == *id {
				return fmt.Errorf("item id %q already exists on %s", *id, slide.ID)
			}
			aboutFound = aboutFound || existing.ID == *about
		}
		if !aboutFound || *about == *id {
			return fmt.Errorf("--about must identify a different existing Item on the same slide")
		}
		fragment := &saga.Fragment{Directory: slide.Directory, MediaType: slide.MediaType, Entrypoint: slide.Entrypoint}
		if err := validateLandmarkSelector(fragment, selector, hotspotRegion); err != nil {
			return err
		}
		if strings.TrimSpace(*description) == "" {
			return fmt.Errorf("--description is required so the visual item has a non-visual equivalent")
		}
		chosenRank := rank.value
		if !rank.set {
			for _, item := range slide.Items {
				if item.Rank >= chosenRank {
					chosenRank = item.Rank + 10
				}
			}
		}
		target = saga.ItemTarget(document.Manifest.ID, slide.ID, *id)
		filename, err := saga.FlatItemFilename(slide.Target, target, chosenRank)
		if err != nil {
			return err
		}
		path := filepath.Join(document.Root, filename)
		manifest := saga.ItemManifest{Version: saga.SlideSagaVersion, ID: *id, SlideID: slide.ID, Rank: chosenRank, Kind: *kind, Label: *label, Description: strings.TrimSpace(*description), Selector: selector, Hotspot: hotspotRegion, About: *about, Body: *body, Placement: *placement, Leader: *leader}
		if err := store.WriteJSON(path, manifest, true); err != nil {
			return err
		}
		slide.ReadingOrder = append(slide.ReadingOrder, *id)
		if err := store.WriteJSON(filepath.Join(document.Root, filepath.FromSlash(slide.Path)), slide.SlideManifest, false); err != nil {
			_ = os.Remove(path)
			return err
		}
		created = filename
		return nil
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Added item %s\nTarget: %s\nNext: change-saga cover --target %s ... %s\n", filepath.ToSlash(created), target, target, flags.Arg(0))
	return nil
}

func SetSlideContent(_ context.Context, args []string, out io.Writer) error {
	flags := commandFlags("set-slide-content", commandUsage["set-slide-content"], out)
	targetValue := flags.String("target", "", "slide path, id, or target URN")
	source := flags.String("source", "", "content file, or - for standard input")
	jsonOutput := flags.Bool("json", false, "emit one machine-readable JSON result")
	quiet := flags.Bool("quiet", false, "suppress successful output")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 || *targetValue == "" || *source == "" {
		return fmt.Errorf("usage: %s", commandUsage["set-slide-content"])
	}
	if *jsonOutput && *quiet {
		return fmt.Errorf("--json and --quiet cannot be combined")
	}
	var data []byte
	var err error
	if *source == "-" {
		data, err = io.ReadAll(os.Stdin)
	} else {
		data, err = os.ReadFile(*source)
	}
	if err != nil {
		return fmt.Errorf("read slide content: %w", err)
	}
	var target string
	err = authorMutation(flags.Arg(0), func(document *saga.Saga) error {
		if err := requireSlideSaga(document, "set-slide-content"); err != nil {
			return err
		}
		slide := findSlide(document, *targetValue)
		if slide == nil {
			return fmt.Errorf("--target must identify a slide")
		}
		entrypoint := filepath.Join(slide.Directory, filepath.FromSlash(slide.Entrypoint))
		if err := store.WriteFile(entrypoint, data, 0o644, false); err != nil {
			return err
		}
		target = slide.Target
		return nil
	})
	if err != nil {
		return err
	}
	if *quiet {
		return nil
	}
	if *jsonOutput {
		return writeJSON(out, map[string]any{"ok": true, "target": target, "bytes": len(data)})
	}
	fmt.Fprintf(out, "Updated %s (%d bytes)\n", target, len(data))
	return nil
}

func findDeck(document *saga.Saga, value string) *saga.Deck {
	for _, deck := range document.Decks {
		if value == deck.ID || value == deck.Target || filepath.Clean(value) == filepath.Clean(deck.Path) || document.Manifest.Version != saga.SlideSagaVersion && filepath.Clean(value) == filepath.Clean(deck.Directory) {
			return deck
		}
	}
	return nil
}

func findSlide(document *saga.Saga, value string) *saga.Slide {
	for _, deck := range document.Decks {
		for _, slide := range deck.Slides {
			if value == slide.ID || value == slide.Target || filepath.Clean(value) == filepath.Clean(slide.Path) || document.Manifest.Version != saga.SlideSagaVersion && filepath.Clean(value) == filepath.Clean(slide.Directory) {
				return slide
			}
		}
	}
	return nil
}

func defaultSlideSVG(title, takeaway string) string {
	title = html.EscapeString(title)
	takeaway = html.EscapeString(takeaway)
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1280 720" role="img" aria-labelledby="slide-title slide-desc">
  <title id="slide-title">%s</title>
  <desc id="slide-desc">%s</desc>
  <rect width="1280" height="720" fill="#f7f7f4"/>
  <text x="80" y="112" font-family="system-ui, sans-serif" font-size="48" font-weight="700" fill="#171717">%s</text>
  <text x="80" y="650" font-family="system-ui, sans-serif" font-size="28" fill="#444">%s</text>
</svg>
`, title, takeaway, title, takeaway)
}

const slideNativeBootstrapREADME = `# Slide-native Change Saga

This is a v4 visual review deck, not a paginated report.

Author with ` + "`change-saga add-deck`" + `, ` + "`change-saga add-slide`" + `, and ` + "`change-saga add-item`" + `.
Every meaningful visual node, edge, region, transition, or callout is an Item.
Attach exact diff evidence to Items with ` + "`change-saga cover`" + `; deck- and slide-level evidence is refused.

Before authoring, storyboard the reviewer question and truthful visual form of
each slide. Use boundaries for systems, containment/dependencies for
architecture, directed edges for data flow, lanes/messages for sequence,
states and labeled transitions for lifecycle, entities and cardinalities for
data models, branches for logic, and trigger/propagation/containment/recovery
for failure paths. A row of labeled cards is not a default diagram.

Audit the deck as a contact sheet before handoff. If slides remain
indistinguishable after labels and colors are ignored—or several unrelated
questions use the same primitive topology—rewrite them before mapping more
evidence. Coverage detects omissions; it cannot turn a weak visual into an explanation.

The package is intentionally flat and compact. Treat category-prefixed filenames
as private storage; use stable IDs, target URNs, and ` + "`change-saga query`" + `.
`
