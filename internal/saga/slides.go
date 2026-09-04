package saga

import (
	"fmt"
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
	decksByKey := map[string]*Deck{}
	slidesByKey := map[string]*Slide{}
	itemsByKey := map[string]*Item{}
	allowedAssets := map[string]bool{}
	regular := map[string]bool{}
	for _, entry := range entries {
		name := entry.Name()
		path := filepath.Join(root, name)
		if issue := flatPathIssue(root, name); issue != "" {
			addIssue(validation, "error", name, issue)
		}
		if !flatRegular(entry) {
			addIssue(validation, "error", name, "v4 is flat and permits only regular files at the Saga root; migrate nested packages explicitly")
			continue
		}
		regular[name] = true
		matches := flatDeckName.FindStringSubmatch(name)
		if matches == nil {
			continue
		}
		var value DeckManifest
		if err := readJSON(path, &value); err != nil {
			addIssue(validation, "error", name, err.Error())
			continue
		}
		deck := &Deck{Path: name, Directory: root, DeckManifest: value, Target: DeckTarget(manifest.ID, value.ID)}
		validateDeckManifest(value, name, deck.Target, validation)
		key := FlatTargetKey(deck.Target)
		if matches[2] != key {
			addIssue(validation, "error", name, "deck filename key does not match its stable target")
		}
		if previous := decksByKey[key]; previous != nil {
			addIssue(validation, "error", name, fmt.Sprintf("deck storage key collides with %s", previous.Path))
		}
		decksByKey[key] = deck
		decks = append(decks, deck)
	}

	for _, entry := range entries {
		name := entry.Name()
		matches := flatSlideName.FindStringSubmatch(name)
		if matches == nil || !regular[name] {
			continue
		}
		path := filepath.Join(root, name)
		var value SlideManifest
		if err := readJSON(path, &value); err != nil {
			addIssue(validation, "error", name, err.Error())
			continue
		}
		deck := decksByKey[matches[1]]
		if deck == nil {
			addIssue(validation, "error", name, "slide filename references an unknown deck key")
			continue
		}
		slide := &Slide{Path: name, Directory: root, SlideManifest: value, Target: SlideTarget(manifest.ID, value.ID)}
		validateSlideManifest(value, name, deck.ID, deck.Target, slide.Target, root, options.outline, validation)
		key := FlatTargetKey(slide.Target)
		if matches[3] != key {
			addIssue(validation, "error", name, "slide filename key does not match its stable target")
		}
		if previous := slidesByKey[key]; previous != nil {
			addIssue(validation, "error", name, fmt.Sprintf("slide storage key collides with %s", previous.Path))
		}
		slidesByKey[key] = slide
		deck.Slides = append(deck.Slides, slide)
		allowedAssets[value.Entrypoint] = true
	}

	// Items are part of a slide's structural outline, not its potentially large
	// evidence graph. Review targets must remain discoverable even in an outline
	// load so an Item comment or decision cannot make the shell invalid.
	for _, entry := range entries {
		name := entry.Name()
		matches := flatItemName.FindStringSubmatch(name)
		if matches == nil || !regular[name] {
			continue
		}
		path := filepath.Join(root, name)
		var value ItemManifest
		if err := readJSON(path, &value); err != nil {
			addIssue(validation, "error", name, err.Error())
			continue
		}
		slide := slidesByKey[matches[1]]
		if slide == nil {
			addIssue(validation, "error", name, "item filename references an unknown slide key")
			continue
		}
		item := &Item{Path: name, Directory: root, ItemManifest: value, Target: ItemTarget(manifest.ID, slide.ID, value.ID)}
		validateItem(item, slide, validation)
		expected, nameErr := FlatItemFilename(slide.Target, item.Target, value.Rank)
		if nameErr != nil || expected != name {
			addIssue(validation, "error", name, "item filename does not match its parent, rank, and stable target")
		}
		key := FlatTargetKey(item.Target)
		if previous := itemsByKey[key]; previous != nil {
			addIssue(validation, "error", name, fmt.Sprintf("item storage key collides with %s", previous.Path))
		}
		itemsByKey[key] = item
		slide.Items = append(slide.Items, item)
	}

	if !options.outline {
		for _, entry := range entries {
			name := entry.Name()
			matches := flatEvidenceName.FindStringSubmatch(name)
			if matches == nil || !regular[name] {
				continue
			}
			item := itemsByKey[matches[1]]
			if item == nil {
				addIssue(validation, "error", name, "evidence filename references an unknown Item key")
				continue
			}
			item.HasDiffs = true
			if options.skipCoverage {
				continue
			}
			var value DiffFile
			if err := readJSON(filepath.Join(root, name), &value); err != nil {
				addIssue(validation, "error", name, err.Error())
				continue
			}
			value.Path = name
			validateDiff(value, validation)
			item.Diffs = append(item.Diffs, value)
		}
	}

	knownRecord := func(name string) bool {
		return name == FlatManifestName || name == "01-readme.md" || name == ".change-saga.lock" || allowedAssets[name] ||
			flatDeckName.MatchString(name) || flatSlideName.MatchString(name) || flatItemName.MatchString(name) ||
			flatEvidenceName.MatchString(name) || flatClaimName.MatchString(name) || flatVerificationName.MatchString(name) ||
			flatThreadName.MatchString(name) || flatMessageName.MatchString(name) || flatAttachmentName.MatchString(name) || flatAttachmentAsset.MatchString(name) ||
			flatThreadEventName.MatchString(name) || flatReviewName.MatchString(name) || flatDiffReviewName.MatchString(name) ||
			strings.HasPrefix(name, ".change-saga-stage-") || strings.HasPrefix(name, ".change-saga-write-")
	}
	for _, entry := range entries {
		if !entry.IsDir() && !knownRecord(entry.Name()) {
			addIssue(validation, "error", entry.Name(), "file does not match the compact v4 storage contract")
		}
	}

	sort.Slice(decks, func(i, j int) bool {
		if decks[i].Rank == decks[j].Rank {
			return decks[i].Path < decks[j].Path
		}
		return decks[i].Rank < decks[j].Rank
	})
	for _, deck := range decks {
		sort.Slice(deck.Slides, func(i, j int) bool {
			if deck.Slides[i].Rank == deck.Slides[j].Rank {
				return deck.Slides[i].Path < deck.Slides[j].Path
			}
			return deck.Slides[i].Rank < deck.Slides[j].Rank
		})
		for _, slide := range deck.Slides {
			sort.Slice(slide.Items, func(i, j int) bool {
				if slide.Items[i].Rank == slide.Items[j].Rank {
					return slide.Items[i].Path < slide.Items[j].Path
				}
				return slide.Items[i].Rank < slide.Items[j].Rank
			})
			if !options.outline {
				validateSlideComposition(slide, validation)
			}
		}
	}
	validateDeckSet(manifest, decks, validation)
	return decks, nil
}

func validateDeckManifest(value DeckManifest, path, target string, validation *Validation) {
	if value.Version != SlideSagaVersion || !stableID.MatchString(value.ID) || strings.TrimSpace(value.Title) == "" {
		addIssue(validation, "error", path, "deck requires version 4, a stable id, and a title")
	}
	expected, err := FlatDeckFilename(target, value.Rank)
	if err != nil || expected != path {
		addIssue(validation, "error", path, "deck filename does not match its rank and stable target")
	}
	if value.Role != "overview" && value.Role != "change" {
		addIssue(validation, "error", path, "deck role must be overview or change")
	}
	if validFlatRank(value.Rank) != nil || strings.TrimSpace(value.Objective) == "" || utf8.RuneCountInString(value.Objective) > 240 {
		addIssue(validation, "error", path, "deck rank must fit the portable range and objective must contain 1 to 240 characters")
	}
}

func validateSlideManifest(value SlideManifest, path, deckID, deckTarget, target, dir string, outline bool, validation *Validation) {
	if value.Version != SlideSagaVersion || !stableID.MatchString(value.ID) || strings.TrimSpace(value.Title) == "" {
		addIssue(validation, "error", path, "slide requires version 4, a stable id, and a title")
	}
	if value.DeckID != deckID {
		addIssue(validation, "error", path, "slide deck must name its semantic parent")
	}
	expected, err := FlatSlideFilename(deckTarget, target, value.Rank)
	if err != nil || expected != path {
		addIssue(validation, "error", path, "slide filename does not match its parent, rank, and stable target")
	}
	if validFlatRank(value.Rank) != nil || !slideIntents[value.Intent] || !slideLayouts[value.Layout] || !slideMediaTypes[value.MediaType] {
		addIssue(validation, "error", path, "slide requires a portable rank and supported intent, layout, and visual media_type")
	}
	if strings.TrimSpace(value.Takeaway) == "" || utf8.RuneCountInString(value.Takeaway) > 180 {
		addIssue(validation, "error", path, "slide takeaway must contain 1 to 180 characters")
	}
	if value.Layout == "custom" && strings.TrimSpace(value.ExceptionRationale) == "" {
		addIssue(validation, "error", path, "custom layout requires exception_rationale")
	}
	extension := filepath.Ext(value.Entrypoint)
	expectedAsset, assetErr := FlatSlideAssetFilename(path, extension)
	if assetErr != nil || expectedAsset != value.Entrypoint {
		addIssue(validation, "error", path, "slide entrypoint must be the compact asset paired with its manifest")
	}
	component := FragmentManifest{Version: CurrentVersion, ID: value.ID, Title: value.Title, MediaType: value.MediaType, Entrypoint: value.Entrypoint, Order: value.Rank}
	validateFragmentManifestMode(component, path, dir, !outline, validation)
}

func validateItem(item *Item, slide *Slide, validation *Validation) {
	if item.Version != SlideSagaVersion || !ValidMarkdownAnchor(item.ID) || !itemKinds[item.Kind] || strings.TrimSpace(item.Label) == "" {
		addIssue(validation, "error", item.Path, "item requires version 4, a lowercase stable id, a supported kind, and a label")
	}
	if item.SlideID != slide.ID {
		addIssue(validation, "error", item.Path, "item slide must name its semantic parent")
	}
	if strings.TrimSpace(item.Description) == "" {
		addIssue(validation, "error", item.Path, "item description is required as its non-visual equivalent")
	}
	if validFlatRank(item.Rank) != nil {
		addIssue(validation, "error", item.Path, "item rank must fit the portable range")
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
		addIssue(validation, "error", FlatManifestName, "presentation.overview_deck does not exist")
	}
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
