package saga

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

var slideIntents = map[string]bool{
	"orient": true, "explain": true, "compare": true, "trace": true,
	"prove": true, "risk": true, "conclude": true,
}

var slideLayouts = map[string]bool{
	"hero": true, "diagram": true, "before-after": true, "sequence": true,
	"evidence": true, "risk": true, "custom": true,
}

var itemKinds = map[string]bool{
	"node": true, "edge": true, "region": true, "transition": true,
	"statement": true, "risk": true, "metric": true, "example": true, "callout": true,
}

var slideMediaTypes = map[string]bool{
	"image/svg+xml": true, "image/png": true, "image/jpeg": true,
	"image/webp": true, "text/html": true,
}

func loadDecks(root string, manifest Manifest, options loadOptions, validation *Validation) ([]*Deck, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var decks []*Deck
	for _, entry := range entries {
		name := entry.Name()
		path := filepath.Join(root, name)
		if strings.HasPrefix(name, "___") {
			if !knownReservedDirectory(name, true, manifest.Version) {
				addIssue(validation, "error", name, "unknown reserved directory in v4 Saga")
			} else if name == "___diffs" {
				addIssue(validation, "error", name, "v4 evidence must be attached to an item, not the Saga")
			} else if name == "___requirements" || name == "___design" || name == "___workplan" {
				addIssue(validation, "error", name, "v4 does not silently reinterpret v3 living-document roots; preserve or migrate them explicitly")
			}
			continue
		}
		if !strings.HasSuffix(name, ".deck") {
			if entry.IsDir() && (strings.HasSuffix(name, ".chapter") || strings.HasSuffix(name, ".section") || strings.HasSuffix(name, ".fragment")) {
				addIssue(validation, "error", name, "v4 slide-native Sagas refuse chapter, section, and fragment packages; migrate them explicitly")
			}
			continue
		}
		if matches, problem := structuralEntry(entry, ".deck"); !matches || problem != "" {
			addIssue(validation, "error", relativePath(root, path), "decks must be real <id>.deck directories")
			continue
		}
		deck, loadErr := loadDeck(root, path, manifest.ID, options, validation)
		if loadErr != nil {
			return nil, loadErr
		}
		decks = append(decks, deck)
	}
	sort.Slice(decks, func(i, j int) bool {
		if decks[i].Rank == decks[j].Rank {
			return decks[i].Path < decks[j].Path
		}
		return decks[i].Rank < decks[j].Rank
	})
	validateDeckSet(manifest, decks, validation)
	return decks, nil
}

func loadDeck(root, dir, sagaID string, options loadOptions, validation *Validation) (*Deck, error) {
	manifestPath := filepath.Join(dir, "deck.json")
	var value DeckManifest
	if err := readJSON(manifestPath, &value); err != nil {
		addIssue(validation, "error", relativePath(root, manifestPath), err.Error())
		value.ID = strings.TrimSuffix(filepath.Base(dir), ".deck")
	}
	rel, _ := filepath.Rel(root, dir)
	deck := &Deck{Path: filepath.ToSlash(rel), Directory: dir, DeckManifest: value, Target: DeckTarget(sagaID, value.ID)}
	validateDeckManifest(value, filepath.Base(dir), relativePath(root, manifestPath), validation)
	if hasRealDirectory(root, dir, "___diffs", validation) {
		addIssue(validation, "error", relativePath(root, filepath.Join(dir, "___diffs")), "v4 evidence must be attached to an item, not a deck")
	}
	if metadataDirectorySafe(root, dir, "___approvals", validation) {
		var err error
		deck.Reviews, err = loadReviews(root, filepath.Join(dir, "___approvals"), validation)
		if err != nil {
			return nil, err
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		name := entry.Name()
		path := filepath.Join(dir, name)
		if strings.HasPrefix(name, "___") {
			if name != "___approvals" && name != "___diffs" {
				addIssue(validation, "error", relativePath(root, path), "unknown reserved directory in deck")
			}
			continue
		}
		if !strings.HasSuffix(name, ".slide") {
			if entry.IsDir() && (strings.HasSuffix(name, ".fragment") || strings.HasSuffix(name, ".section")) {
				addIssue(validation, "error", relativePath(root, path), "decks may contain only .slide packages")
			}
			continue
		}
		if matches, problem := structuralEntry(entry, ".slide"); !matches || problem != "" {
			addIssue(validation, "error", relativePath(root, path), "slides must be real <id>.slide directories")
			continue
		}
		slide, loadErr := loadSlide(root, path, sagaID, options, validation)
		if loadErr != nil {
			return nil, loadErr
		}
		deck.Slides = append(deck.Slides, slide)
	}
	sort.Slice(deck.Slides, func(i, j int) bool {
		if deck.Slides[i].Rank == deck.Slides[j].Rank {
			return deck.Slides[i].Path < deck.Slides[j].Path
		}
		return deck.Slides[i].Rank < deck.Slides[j].Rank
	})
	return deck, nil
}

func loadSlide(root, dir, sagaID string, options loadOptions, validation *Validation) (*Slide, error) {
	manifestPath := filepath.Join(dir, "slide.json")
	var value SlideManifest
	if err := readJSON(manifestPath, &value); err != nil {
		addIssue(validation, "error", relativePath(root, manifestPath), err.Error())
		value.ID = strings.TrimSuffix(filepath.Base(dir), ".slide")
	}
	rel, _ := filepath.Rel(root, dir)
	slide := &Slide{Path: filepath.ToSlash(rel), Directory: dir, SlideManifest: value, Target: SlideTarget(sagaID, value.ID)}
	validateSlideManifest(value, filepath.Base(dir), relativePath(root, manifestPath), dir, options.outline, validation)
	if hasRealDirectory(root, dir, "___diffs", validation) {
		addIssue(validation, "error", relativePath(root, filepath.Join(dir, "___diffs")), "v4 evidence must be attached to an item, not a slide")
	}
	if metadataDirectorySafe(root, dir, "___approvals", validation) {
		var err error
		slide.Reviews, err = loadReviews(root, filepath.Join(dir, "___approvals"), validation)
		if err != nil {
			return nil, err
		}
	}
	itemsDir := filepath.Join(dir, "___items")
	if !options.outline && metadataDirectorySafe(root, dir, "___items", validation) {
		var err error
		slide.Items, err = loadItems(root, itemsDir, sagaID, slide, options, validation)
		if err != nil {
			return nil, err
		}
		validateSlideComposition(slide, validation)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), "___") && entry.Name() != "___items" && entry.Name() != "___approvals" && entry.Name() != "___diffs" {
			addIssue(validation, "error", relativePath(root, filepath.Join(dir, entry.Name())), "unknown reserved directory in slide")
		}
	}
	return slide, nil
}

func loadItems(root, dir, sagaID string, slide *Slide, options loadOptions, validation *Validation) ([]*Item, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var items []*Item
	seen := map[string]string{}
	for _, entry := range entries {
		entryPath := filepath.Join(dir, entry.Name())
		if matches, problem := structuralEntry(entry, ".item"); !matches || problem != "" {
			addIssue(validation, "error", relativePath(root, entryPath), "items must be real <id>.item directories")
			continue
		}
		path := filepath.Join(entryPath, "item.json")
		var value ItemManifest
		if err := readJSON(path, &value); err != nil {
			addIssue(validation, "error", relativePath(root, path), err.Error())
			continue
		}
		item := &Item{Path: relativePath(root, path), Directory: entryPath, ItemManifest: value, Target: ItemTarget(sagaID, slide.ID, value.ID)}
		if directoryID := strings.TrimSuffix(entry.Name(), ".item"); directoryID != value.ID {
			addIssue(validation, "error", item.Path, fmt.Sprintf("item id %q must match directory %q", value.ID, directoryID+".item"))
		}
		validateItem(item, slide, validation)
		if previous, ok := seen[value.ID]; ok {
			addIssue(validation, "error", item.Path, fmt.Sprintf("item id %q is duplicated; first used by %s", value.ID, previous))
		}
		seen[value.ID] = item.Path
		if metadataDirectorySafe(root, entryPath, "___diffs", validation) {
			if !options.skipCoverage {
				item.Diffs, err = loadDiffs(root, filepath.Join(entryPath, "___diffs"), validation)
				if err != nil {
					return nil, err
				}
			}
			if info, statErr := os.Stat(filepath.Join(entryPath, "___diffs")); statErr == nil && info.IsDir() {
				item.HasDiffs = true
			}
		}
		if metadataDirectorySafe(root, entryPath, "___approvals", validation) {
			item.Reviews, err = loadReviews(root, filepath.Join(entryPath, "___approvals"), validation)
			if err != nil {
				return nil, err
			}
		}
		itemEntries, readErr := os.ReadDir(entryPath)
		if readErr != nil {
			return nil, readErr
		}
		for _, itemEntry := range itemEntries {
			if itemEntry.IsDir() && strings.HasPrefix(itemEntry.Name(), "___") && itemEntry.Name() != "___diffs" && itemEntry.Name() != "___approvals" {
				addIssue(validation, "error", relativePath(root, filepath.Join(entryPath, itemEntry.Name())), "unknown reserved directory in item")
			}
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

func validateDeckManifest(value DeckManifest, dirname, path string, validation *Validation) {
	if value.Version != SlideSagaVersion || !stableID.MatchString(value.ID) || strings.TrimSpace(value.Title) == "" {
		addIssue(validation, "error", path, "deck requires version 4, a stable id, and a title")
	}
	if strings.TrimSuffix(dirname, ".deck") != value.ID {
		addIssue(validation, "error", path, "deck id must match its directory name")
	}
	if value.Role != "overview" && value.Role != "change" {
		addIssue(validation, "error", path, "deck role must be overview or change")
	}
	if value.Rank < 0 || strings.TrimSpace(value.Objective) == "" || utf8.RuneCountInString(value.Objective) > 240 {
		addIssue(validation, "error", path, "deck rank must be non-negative and objective must contain 1 to 240 characters")
	}
}

func validateSlideManifest(value SlideManifest, dirname, path, dir string, outline bool, validation *Validation) {
	if value.Version != SlideSagaVersion || !stableID.MatchString(value.ID) || strings.TrimSpace(value.Title) == "" {
		addIssue(validation, "error", path, "slide requires version 4, a stable id, and a title")
	}
	if strings.TrimSuffix(dirname, ".slide") != value.ID {
		addIssue(validation, "error", path, "slide id must match its directory name")
	}
	if value.Rank < 0 || !slideIntents[value.Intent] || !slideLayouts[value.Layout] || !slideMediaTypes[value.MediaType] {
		addIssue(validation, "error", path, "slide requires a non-negative rank and supported intent, layout, and visual media_type")
	}
	if strings.TrimSpace(value.Takeaway) == "" || utf8.RuneCountInString(value.Takeaway) > 180 {
		addIssue(validation, "error", path, "slide takeaway must contain 1 to 180 characters")
	}
	if value.Layout == "custom" && strings.TrimSpace(value.ExceptionRationale) == "" {
		addIssue(validation, "error", path, "custom layout requires exception_rationale")
	}
	component := FragmentManifest{Version: CurrentVersion, ID: value.ID, Title: value.Title, MediaType: value.MediaType, Entrypoint: value.Entrypoint, Order: value.Rank}
	validateFragmentManifestMode(component, path, dir, !outline, validation)
}

func validateItem(item *Item, slide *Slide, validation *Validation) {
	if item.Version != SlideSagaVersion || !ValidMarkdownAnchor(item.ID) || !itemKinds[item.Kind] || strings.TrimSpace(item.Label) == "" {
		addIssue(validation, "error", item.Path, "item requires version 4, a lowercase stable id, a supported kind, and a label")
	}
	if strings.TrimSpace(item.Description) == "" {
		addIssue(validation, "error", item.Path, "item description is required as its non-visual equivalent")
	}
	if utf8.RuneCountInString(item.Label) > 100 || utf8.RuneCountInString(item.Description) > 240 {
		addIssue(validation, "error", item.Path, "item label and description exceed the 100/240 character density limits")
	}
	if item.Kind == "callout" {
		if strings.TrimSpace(item.Body) == "" || utf8.RuneCountInString(item.Body) > 240 {
			addIssue(validation, "error", item.Path, "callout item body must contain 1 to 240 characters")
		}
		if item.Placement != "" && item.Placement != "top" && item.Placement != "right" && item.Placement != "bottom" && item.Placement != "left" && item.Placement != "overlay" {
			addIssue(validation, "error", item.Path, "callout placement is not supported")
		}
		if item.Leader != "" && item.Leader != "none" && item.Leader != "line" && item.Leader != "arrow" {
			addIssue(validation, "error", item.Path, "callout leader is not supported")
		}
	} else if item.About != "" || item.Body != "" || item.Placement != "" || item.Leader != "" {
		addIssue(validation, "error", item.Path, "about, body, placement, and leader are callout-only fields")
	}
	legacy := Landmark{Path: item.Path, Directory: item.Directory, Version: CurrentVersion, ID: item.ID, Label: item.Label, Description: item.Description, Selector: item.Selector, Hotspot: item.Hotspot}
	fragment := &Fragment{Directory: slide.Directory, ID: slide.ID, MediaType: slide.MediaType, Entrypoint: slide.Entrypoint}
	firstSelectorIssue := len(validation.Issues)
	validateLandmark(legacy, fragment, validation)
	for index := firstSelectorIssue; index < len(validation.Issues); index++ {
		validation.Issues[index].Message = strings.NewReplacer("landmark", "item", "fragment", "slide").Replace(validation.Issues[index].Message)
	}
}

func validateSlideComposition(slide *Slide, validation *Validation) {
	if len(slide.Items) == 0 {
		addIssue(validation, "error", slide.Path, "slide has no semantic items; every slide must expose its meaningful visual elements")
	}
	if len(slide.Items) > 7 {
		severity := "error"
		if slide.Layout == "custom" && strings.TrimSpace(slide.ExceptionRationale) != "" {
			severity = "warning"
		}
		addIssue(validation, severity, slide.Path, "standard slide layouts allow at most 7 semantic items; split the slide or use a justified custom layout")
	}
	items := map[string]*Item{}
	for _, item := range slide.Items {
		items[item.ID] = item
	}
	seen := map[string]bool{}
	for _, id := range slide.ReadingOrder {
		if seen[id] {
			addIssue(validation, "error", slide.Path, fmt.Sprintf("reading_order repeats item %q", id))
		} else if items[id] == nil {
			addIssue(validation, "error", slide.Path, fmt.Sprintf("reading_order references unknown item %q", id))
		}
		seen[id] = true
	}
	for _, item := range slide.Items {
		if !seen[item.ID] {
			addIssue(validation, "error", item.Path, "every semantic item must appear in slide reading_order")
		}
		if item.Kind == "callout" && item.About != "" {
			if item.About == item.ID || items[item.About] == nil {
				addIssue(validation, "error", item.Path, "callout about must name a different item on the same slide")
			}
		}
	}
}

func validateDeckSet(manifest Manifest, decks []*Deck, validation *Validation) {
	overviewCount := 0
	foundConfigured := false
	deckRanks := map[int]string{}
	for _, deck := range decks {
		if previous, exists := deckRanks[deck.Rank]; exists {
			addIssue(validation, "warning", deck.Path, fmt.Sprintf("deck rank %d is also used by %s; path order is the deterministic tie-break", deck.Rank, previous))
		} else {
			deckRanks[deck.Rank] = deck.Path
		}
		slideRanks := map[int]string{}
		for _, slide := range deck.Slides {
			if previous, exists := slideRanks[slide.Rank]; exists {
				addIssue(validation, "warning", slide.Path, fmt.Sprintf("slide rank %d is also used by %s; path order is the deterministic tie-break", slide.Rank, previous))
			} else {
				slideRanks[slide.Rank] = slide.Path
			}
		}
		if deck.Role == "overview" {
			overviewCount++
		}
		if manifest.Presentation != nil && deck.ID == manifest.Presentation.OverviewDeck {
			foundConfigured = true
			if deck.Role != "overview" {
				addIssue(validation, "error", deck.Path, "presentation.overview_deck must name a deck with role overview")
			}
		}
	}
	if overviewCount != 1 {
		addIssue(validation, "error", ".", "v4 requires exactly one overview deck")
	}
	if !foundConfigured {
		addIssue(validation, "error", "saga.json", "presentation.overview_deck does not exist")
	}
}

func hasRealDirectory(root, dir, name string, validation *Validation) bool {
	path := filepath.Join(dir, name)
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false
	}
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		addIssue(validation, "error", relativePath(root, path), "reserved metadata path must be a real directory")
		return false
	}
	return true
}

// projectDecks is the compatibility boundary inside the implementation. It
// lets established review/evidence plumbing operate on v4 targets without ever
// pretending that the on-disk document is a paginated report.
func projectDecks(manifest Manifest, decks []*Deck) *Section {
	root := &Section{Kind: "saga", ID: manifest.ID + "-root", Title: manifest.Title, Target: SagaTarget(manifest.ID)}
	for _, deck := range decks {
		section := &Section{Path: deck.Path, Kind: "deck", ID: deck.ID, Title: deck.Title, Order: deck.Rank, Target: deck.Target, Reviews: deck.Reviews}
		for _, slide := range deck.Slides {
			meta := slide.SlideManifest
			fragment := &Fragment{Path: slide.Path, Directory: slide.Directory, ID: slide.ID, Title: slide.Title, MediaType: slide.MediaType, Entrypoint: slide.Entrypoint, Order: slide.Rank, Target: slide.Target, Reviews: slide.Reviews, SlideMeta: &meta}
			for _, item := range slide.Items {
				meta := item.ItemManifest
				fragment.Landmarks = append(fragment.Landmarks, Landmark{Path: item.Path, Directory: item.Directory, Version: item.Version, ID: item.ID, Label: item.Label, Description: item.Description, Selector: item.Selector, Hotspot: item.Hotspot, Target: item.Target, Diffs: item.Diffs, HasDiffs: item.HasDiffs, ItemMeta: &meta, Reviews: item.Reviews})
			}
			section.Fragments = append(section.Fragments, fragment)
		}
		root.Children = append(root.Children, section)
	}
	return root
}
