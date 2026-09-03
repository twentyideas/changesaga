package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/twentyideas/changesaga/internal/saga"
	"github.com/twentyideas/changesaga/internal/store"
)

// AddLandmark creates an addressable place inside a fragment. Coverage can
// then target the returned path or URN with the ordinary cover command.
func AddLandmark(_ context.Context, args []string, out io.Writer) error {
	flags := commandFlags("add-landmark", commandUsage["add-landmark"], out)
	target := flags.String("target", "", "containing .fragment directory or fragment URN")
	id := flags.String("id", "", "stable landmark identifier")
	label := flags.String("label", "", "reviewer-facing landmark label")
	description := flags.String("description", "", "semantic explanation for AI and non-visual consumers")
	elementID := flags.String("element-id", "", "id of an HTML or SVG element; SVG bounds become an on-canvas link automatically")
	headingID := flags.String("heading-id", "", "explicit Markdown heading anchor; must equal --id when --id is provided")
	exact := flags.String("text", "", "exact text to mark in a Markdown or text fragment")
	prefix := flags.String("prefix", "", "text immediately before --text, for disambiguation")
	suffix := flags.String("suffix", "", "text immediately after --text, for disambiguation")
	region := flags.String("region", "", "normalized image region x,y,width,height")
	hotspot := flags.String("hotspot", "", "normalized on-canvas hit area x,y,width,height; overrides inferred SVG element bounds")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 || strings.TrimSpace(*target) == "" {
		return fmt.Errorf("usage: change-saga add-landmark --target <fragment> [selector] [flags] <saga>")
	}

	selectorCount := 0
	for _, value := range []string{*elementID, *headingID, *exact, *region} {
		if value != "" {
			selectorCount++
		}
	}
	if selectorCount != 1 {
		return fmt.Errorf("provide exactly one selector: --element-id, --heading-id, --text, or --region")
	}
	if *exact == "" && (*prefix != "" || *suffix != "") {
		return fmt.Errorf("--prefix and --suffix may only be used with --text")
	}

	var selector saga.LandmarkSelector
	defaultID := ""
	switch {
	case *elementID != "":
		selector = saga.LandmarkSelector{Type: "element", ElementID: *elementID}
		defaultID = *elementID
	case *headingID != "":
		selector = saga.LandmarkSelector{Type: "heading", HeadingID: *headingID}
		defaultID = *headingID
	case *exact != "":
		selector = saga.LandmarkSelector{Type: "text", Exact: *exact, Prefix: *prefix, Suffix: *suffix}
		defaultID = store.Slug(*exact)
	case *region != "":
		parsed, err := parseLandmarkRegion(*region)
		if err != nil {
			return fmt.Errorf("invalid --region: %w", err)
		}
		selector = saga.LandmarkSelector{Type: "region", X: parsed.X, Y: parsed.Y, Width: parsed.Width, Height: parsed.Height}
		defaultID = "region"
	}
	if *id == "" {
		*id = defaultID
	}
	if !saga.ValidMarkdownAnchor(*id) {
		return fmt.Errorf("--id must begin with a lowercase letter and contain only lowercase letters, digits, and hyphens (maximum 64 characters)")
	}
	if selector.Type == "heading" && *id != selector.HeadingID {
		return fmt.Errorf("a heading landmark --id must match its --heading-id")
	}
	if *label == "" {
		*label = strings.ReplaceAll(*id, "-", " ")
	}
	if strings.TrimSpace(*label) == "" {
		return fmt.Errorf("--label cannot be empty")
	}
	var hotspotRegion *saga.LandmarkRegion
	if *hotspot != "" {
		parsed, err := parseLandmarkRegion(*hotspot)
		if err != nil {
			return fmt.Errorf("invalid --hotspot: %w", err)
		}
		hotspotRegion = &parsed
	}

	var created, targetURN, createdMediaType string
	err := authorMutation(flags.Arg(0), func(document *saga.Saga) error {
		if document.Manifest.Version == saga.SlideSagaVersion {
			return fmt.Errorf("add-landmark is unavailable for v4 slide-native Sagas; use add-item")
		}
		targetDir, resolved, err := resolveTarget(document, *target, true)
		if err != nil {
			return err
		}
		fragment := fragmentAtDirectory(document.Section, targetDir)
		if fragment == nil || resolved != fragment.Target {
			return fmt.Errorf("--target must identify a fragment, not a saga, chapter, section, or existing landmark")
		}
		for _, existing := range fragment.Landmarks {
			if existing.ID == *id {
				return fmt.Errorf("landmark id %q already exists in %s", *id, fragment.Path)
			}
		}
		if err := validateLandmarkSelector(fragment, selector, hotspotRegion); err != nil {
			return err
		}
		if (fragment.MediaType == "text/html" || strings.HasPrefix(fragment.MediaType, "image/")) && strings.TrimSpace(*description) == "" {
			return fmt.Errorf("--description is required for a visual landmark so non-visual consumers can understand its meaning")
		}
		landmarksDir, err := store.EnsureDirWithin(document.Root, filepath.Join(fragment.Directory, "___landmarks"))
		if err != nil {
			return err
		}
		dir := filepath.Join(landmarksDir, *id+".landmark")
		manifest := saga.Landmark{Version: saga.CurrentVersion, ID: *id, Label: *label, Description: strings.TrimSpace(*description), Selector: selector, Hotspot: hotspotRegion}
		err = store.CommitDir(document.Root, dir, func(stage string) error {
			if err := os.Chmod(stage, 0o755); err != nil {
				return err
			}
			if err := os.Mkdir(filepath.Join(stage, "___diffs"), 0o755); err != nil {
				return err
			}
			return store.WriteJSON(filepath.Join(stage, "landmark.json"), manifest, true)
		})
		if errors.Is(err, fs.ErrExist) {
			return fmt.Errorf("landmark id %q already exists in %s", *id, fragment.Path)
		}
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(document.Root, dir)
		created = filepath.ToSlash(rel)
		targetURN = saga.LandmarkTarget(document.Manifest.ID, fragment.ID, *id)
		createdMediaType = fragment.MediaType
		return nil
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Added landmark %s\nTarget: %s\n", created, targetURN)
	if selector.Type == "element" {
		if hotspotRegion != nil {
			fmt.Fprintln(out, "On-canvas link: uses the explicit --hotspot geometry.")
		} else if createdMediaType == "image/svg+xml" {
			fmt.Fprintf(out, "On-canvas link: inferred from SVG element #%s; use --hotspot only to override its bounds.\n", selector.ElementID)
		} else {
			fmt.Fprintln(out, "On-canvas link: HTML elements need --hotspot geometry; without it they remain available through Marked places and deep links.")
		}
	}
	fmt.Fprintf(out, "Next: change-saga cover --target %q ... %s\n", created, flags.Arg(0))
	return nil
}

func fragmentAtDirectory(section *saga.Section, directory string) *saga.Fragment {
	wanted, _ := filepath.Abs(directory)
	for _, fragment := range section.Fragments {
		candidate, _ := filepath.Abs(fragment.Directory)
		if candidate == wanted {
			return fragment
		}
	}
	for _, child := range section.Children {
		if fragment := fragmentAtDirectory(child, directory); fragment != nil {
			return fragment
		}
	}
	return nil
}

func parseLandmarkRegion(value string) (saga.LandmarkRegion, error) {
	parts := strings.Split(value, ",")
	if len(parts) != 4 {
		return saga.LandmarkRegion{}, fmt.Errorf("expected x,y,width,height")
	}
	values := make([]float64, 4)
	for index, part := range parts {
		parsed, err := strconv.ParseFloat(strings.TrimSpace(part), 64)
		if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
			return saga.LandmarkRegion{}, fmt.Errorf("coordinate %d is not a number", index+1)
		}
		values[index] = parsed
	}
	region := saga.LandmarkRegion{X: values[0], Y: values[1], Width: values[2], Height: values[3]}
	if region.X < 0 || region.Y < 0 || region.Width <= 0 || region.Height <= 0 || region.X+region.Width > 1 || region.Y+region.Height > 1 {
		return saga.LandmarkRegion{}, fmt.Errorf("coordinates must define a positive normalized rectangle within 0..1")
	}
	return region, nil
}

func validateLandmarkSelector(fragment *saga.Fragment, selector saga.LandmarkSelector, hotspot *saga.LandmarkRegion) error {
	entrypoint, err := os.ReadFile(filepath.Join(fragment.Directory, filepath.FromSlash(fragment.Entrypoint)))
	if err != nil {
		return fmt.Errorf("read fragment entrypoint: %w", err)
	}
	content := string(entrypoint)
	switch selector.Type {
	case "element":
		if fragment.MediaType != "text/html" && fragment.MediaType != "image/svg+xml" {
			return fmt.Errorf("--element-id requires an HTML or SVG fragment")
		}
		if !saga.ValidMarkdownAnchor(selector.ElementID) {
			return fmt.Errorf("--element-id must use lowercase letters, digits, and hyphens and begin with a letter")
		}
		if !strings.Contains(content, `id="`+selector.ElementID+`"`) && !strings.Contains(content, `id='`+selector.ElementID+`'`) {
			return fmt.Errorf("element id %q does not appear in the fragment entrypoint", selector.ElementID)
		}
	case "heading":
		if fragment.MediaType != "text/markdown" {
			return fmt.Errorf("--heading-id requires a Markdown fragment")
		}
		found := false
		for _, heading := range saga.MarkdownHeadings(content) {
			found = found || heading.Explicit && heading.Anchor == selector.HeadingID
		}
		if !found {
			return fmt.Errorf("heading anchor %q does not appear in the fragment entrypoint", selector.HeadingID)
		}
	case "text":
		if fragment.MediaType != "text/markdown" && fragment.MediaType != "text/plain" {
			return fmt.Errorf("--text requires a Markdown or plain-text fragment")
		}
		needle := selector.Prefix + selector.Exact + selector.Suffix
		if !strings.Contains(content, needle) {
			return fmt.Errorf("the requested text and context do not appear in the fragment entrypoint")
		}
	case "region":
		if !strings.HasPrefix(fragment.MediaType, "image/") {
			return fmt.Errorf("--region requires an image fragment")
		}
	}
	if hotspot != nil && fragment.MediaType != "text/html" && !strings.HasPrefix(fragment.MediaType, "image/") {
		return fmt.Errorf("--hotspot requires an HTML, image, or SVG fragment")
	}
	return nil
}
