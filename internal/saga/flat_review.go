package saga

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/twentyideas/changesaga/internal/diffuri"
)

func loadFlatReviewState(index MutationIndex, outline bool) (ReviewState, Validation, error) {
	validation := Validation{Valid: true, Issues: []Issue{}}
	state := ReviewState{ByTarget: map[string][]Review{}}
	entries, err := os.ReadDir(index.Root)
	if err != nil {
		return state, validation, err
	}
	targets := map[string]string{}
	for target := range index.Targets {
		targets[FlatTargetKey(target)] = target
	}
	reviewTargets := map[string]string{}
	for target := range index.ReviewTargets {
		reviewTargets[FlatTargetKey(target)] = target
	}
	threads := map[string]*Thread{}
	for _, entry := range entries {
		match := flatThreadName.FindStringSubmatch(entry.Name())
		if match == nil || !flatRegular(entry) {
			continue
		}
		path := filepath.Join(index.Root, entry.Name())
		var manifest ThreadManifest
		if err := readJSON(path, &manifest); err != nil {
			addIssue(&validation, "error", entry.Name(), err.Error())
			continue
		}
		if expected := FlatThreadFilename(manifest.Target, manifest.ID); expected != entry.Name() || targets[match[1]] != manifest.Target {
			addIssue(&validation, "error", entry.Name(), "thread filename keys do not match its target and stable id")
		}
		thread := &Thread{Path: path, Version: manifest.Version, ID: manifest.ID, Target: manifest.Target, Anchor: manifest.Anchor, Kind: manifest.Kind, Suggestion: manifest.Suggestion, CreatedBy: manifest.CreatedBy, CreatedAt: manifest.CreatedAt, Directory: index.Root, State: "open"}
		validateThread(*thread, index.Manifest.ID, entry.Name(), &validation)
		threads[FlatKey("thread\x00"+thread.ID)] = thread
		state.Threads = append(state.Threads, thread)
	}

	if !outline {
		messages := map[string]*Message{}
		for _, entry := range entries {
			match := flatMessageName.FindStringSubmatch(entry.Name())
			if match == nil || !flatRegular(entry) {
				continue
			}
			thread := threads[match[1]]
			if thread == nil {
				addIssue(&validation, "error", entry.Name(), "message filename references an unknown thread key")
				continue
			}
			path := filepath.Join(index.Root, entry.Name())
			var manifest MessageManifest
			if err := readJSON(path, &manifest); err != nil {
				addIssue(&validation, "error", entry.Name(), err.Error())
				continue
			}
			if expected := FlatMessageFilename(thread.ID, manifest.ID); expected != entry.Name() {
				addIssue(&validation, "error", entry.Name(), "message filename keys do not match its thread and stable id")
			}
			if manifest.Version != CurrentVersion || !stableID.MatchString(manifest.ID) || manifest.CreatedAt.IsZero() {
				addIssue(&validation, "error", entry.Name(), "message requires version 2, a stable id, and created_at")
			}
			message := &Message{Path: path, ID: manifest.ID, Author: manifest.Author, CreatedAt: manifest.CreatedAt}
			messages[FlatKey("message\x00"+message.ID)] = message
			thread.Messages = append(thread.Messages, message)
		}
		for _, entry := range entries {
			match := flatAttachmentName.FindStringSubmatch(entry.Name())
			if match == nil || !flatRegular(entry) {
				continue
			}
			message := messages[match[1]]
			if message == nil {
				addIssue(&validation, "error", entry.Name(), "review attachment references an unknown message key")
				continue
			}
			path := filepath.Join(index.Root, entry.Name())
			var manifest FragmentManifest
			if err := readJSON(path, &manifest); err != nil {
				addIssue(&validation, "error", entry.Name(), err.Error())
				continue
			}
			order, _ := strconv.Atoi(match[2])
			expected, _ := FlatAttachmentFilename(message.ID, order, manifest.ID)
			if expected != entry.Name() || manifest.Order != order {
				addIssue(&validation, "error", entry.Name(), "review attachment filename does not match its message, order, and stable id")
			}
			extension := filepath.Ext(manifest.Entrypoint)
			expectedAsset, assetErr := FlatSlideAssetFilename(entry.Name(), extension)
			if assetErr != nil || expectedAsset != manifest.Entrypoint {
				addIssue(&validation, "error", entry.Name(), "review attachment entrypoint must share its compact manifest stem")
			}
			fragment := &Fragment{Path: entry.Name(), Directory: index.Root, ID: manifest.ID, Title: manifest.Title, MediaType: manifest.MediaType, Entrypoint: manifest.Entrypoint, Order: manifest.Order, Target: FragmentTarget(index.Manifest.ID, manifest.ID)}
			validateFragmentManifestMode(manifest, entry.Name(), index.Root, true, &validation)
			message.Fragments = append(message.Fragments, fragment)
		}
	}

	for _, entry := range entries {
		if match := flatThreadEventName.FindStringSubmatch(entry.Name()); match != nil && flatRegular(entry) {
			thread := threads[match[1]]
			if thread == nil {
				addIssue(&validation, "error", entry.Name(), "thread event references an unknown thread key")
				continue
			}
			var event ThreadEvent
			if err := readJSON(filepath.Join(index.Root, entry.Name()), &event); err != nil {
				addIssue(&validation, "error", entry.Name(), err.Error())
				continue
			}
			event.Path = filepath.Join(index.Root, entry.Name())
			if FlatThreadEventFilename(thread.ID, event.ID) != entry.Name() {
				addIssue(&validation, "error", entry.Name(), "thread event filename keys do not match its thread and stable id")
			}
			validState := event.State == "" || event.State == "open" || event.State == "resolved" || event.State == "withdrawn"
			if event.Version != CurrentVersion || !stableID.MatchString(event.ID) || event.CreatedAt.IsZero() || !validState || event.State == "" && event.Anchor == nil {
				addIssue(&validation, "error", entry.Name(), "thread event requires version 2, a stable id, created_at, and a valid state or anchor")
			}
			thread.Events = append(thread.Events, event)
		} else if match := flatReviewName.FindStringSubmatch(entry.Name()); match != nil && flatRegular(entry) {
			target := reviewTargets[match[1]]
			if target == "" {
				addIssue(&validation, "error", entry.Name(), "v4 decisions must target a slide")
				continue
			}
			var review Review
			if err := readJSON(filepath.Join(index.Root, entry.Name()), &review); err != nil {
				addIssue(&validation, "error", entry.Name(), err.Error())
				continue
			}
			review.Path = filepath.Join(index.Root, entry.Name())
			if FlatReviewFilename(target, review.ID) != entry.Name() || review.Version != CurrentVersion || !stableID.MatchString(review.ID) || review.CreatedAt.IsZero() || !validReviewState(review.State) {
				addIssue(&validation, "error", entry.Name(), "review filename and record must match a valid target, id, timestamp, and state")
			}
			state.ByTarget[target] = append(state.ByTarget[target], review)
		} else if flatDiffReviewName.MatchString(entry.Name()) && flatRegular(entry) {
			var review DiffReview
			if err := readJSON(filepath.Join(index.Root, entry.Name()), &review); err != nil {
				addIssue(&validation, "error", entry.Name(), err.Error())
				continue
			}
			reference, uriErr := diffuri.Parse(review.URI)
			review.Path = filepath.Join(index.Root, entry.Name())
			if FlatDiffReviewFilename(review.ID) != entry.Name() || review.Version != CurrentVersion || !stableID.MatchString(review.ID) || review.CreatedAt.IsZero() || review.State != "reviewed" && review.State != "unreviewed" || uriErr != nil || reference.Kind != "file" {
				addIssue(&validation, "error", entry.Name(), "diff review filename and record must match a valid file decision")
			}
			state.DiffReviews = append(state.DiffReviews, review)
		}
	}

	for _, thread := range state.Threads {
		sort.Slice(thread.Messages, func(i, j int) bool {
			return earlierRecord(thread.Messages[i].CreatedAt, thread.Messages[i].ID, thread.Messages[j].CreatedAt, thread.Messages[j].ID)
		})
		for _, message := range thread.Messages {
			sort.Slice(message.Fragments, func(i, j int) bool { return message.Fragments[i].Order < message.Fragments[j].Order })
		}
		if !outline && len(thread.Messages) == 0 {
			addIssue(&validation, "error", filepath.Base(thread.Path), "thread must contain at least one message")
		}
		sort.Slice(thread.Events, func(i, j int) bool {
			return earlierRecord(thread.Events[i].CreatedAt, thread.Events[i].ID, thread.Events[j].CreatedAt, thread.Events[j].ID)
		})
		for _, event := range thread.Events {
			if event.State != "" {
				thread.State = event.State
			}
			if event.Anchor != nil {
				thread.Anchor = *event.Anchor
			}
		}
		if thread.Anchor.Type == "diff" && thread.Anchor.Diff != nil {
			reference, parseErr := diffuri.Parse(thread.Anchor.Diff.URI)
			repository, repositoryErr := diffuri.CanonicalRepository(index.Manifest.Source.Repository)
			if parseErr == nil && repositoryErr == nil && reference.Repository != repository {
				addIssue(&validation, "error", filepath.Base(thread.Path), "thread diff anchor belongs to a different source repository")
			}
		}
	}
	sort.Slice(state.Threads, func(i, j int) bool {
		return earlierRecord(state.Threads[i].CreatedAt, state.Threads[i].ID, state.Threads[j].CreatedAt, state.Threads[j].ID)
	})
	for target := range state.ByTarget {
		sort.Slice(state.ByTarget[target], func(i, j int) bool {
			return earlierRecord(state.ByTarget[target][i].CreatedAt, state.ByTarget[target][i].ID, state.ByTarget[target][j].CreatedAt, state.ByTarget[target][j].ID)
		})
	}
	validation.Valid = !hasErrors(validation.Issues)
	return state, validation, nil
}

func applyFlatReviews(section *Section, byTarget map[string][]Review) {
	section.Reviews = byTarget[section.Target]
	for _, fragment := range section.Fragments {
		fragment.Reviews = byTarget[fragment.Target]
		for index := range fragment.Landmarks {
			fragment.Landmarks[index].Reviews = byTarget[fragment.Landmarks[index].Target]
		}
	}
	for _, child := range section.Children {
		applyFlatReviews(child, byTarget)
	}
}

func flatReviewFilesForMessage(root, messageID string) ([]string, error) {
	prefix := "82-a-" + FlatKey("message\x00"+messageID) + "-"
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), prefix) {
			paths = append(paths, filepath.Join(root, entry.Name()))
		}
	}
	return paths, nil
}
