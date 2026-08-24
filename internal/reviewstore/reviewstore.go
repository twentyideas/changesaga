package reviewstore

import (
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/twentyideas/changesaga/internal/diffuri"
	"github.com/twentyideas/changesaga/internal/saga"
	"github.com/twentyideas/changesaga/internal/store"
)

var mutationFaultHook func(string) error

func AddThread(root, target, body string, anchor saga.Anchor, kind, replacement string, attachments []string) (id string, err error) {
	if target == "" {
		return "", fmt.Errorf("target is required")
	}
	if err := saga.ValidateAnchor(anchor); err != nil {
		return "", err
	}
	if kind == "" {
		kind = "comment"
	}
	if kind != "comment" && kind != "suggestion" {
		return "", fmt.Errorf("thread kind must be comment or suggestion")
	}
	if kind == "suggestion" && (anchor.Type != "diff" || strings.TrimSpace(replacement) == "") {
		return "", fmt.Errorf("suggestions require a diff anchor and replacement content")
	}
	if kind != "suggestion" && strings.TrimSpace(replacement) != "" {
		return "", fmt.Errorf("replacement content is only valid for suggestions")
	}
	if strings.TrimSpace(body) == "" && len(attachments) == 0 {
		return "", fmt.Errorf("message body or attachment is required")
	}
	if err := validateAttachments(attachments); err != nil {
		return "", err
	}
	now := time.Now().UTC()
	id = store.EventID(now)
	err = mutate(root, func(index saga.MutationIndex) error {
		if _, ok := index.Targets[target]; !ok {
			return fmt.Errorf("target does not exist")
		}
		if err := verifyAnchorRepository(index, anchor); err != nil {
			return err
		}
		threadsDir, err := store.EnsureDirWithin(root, filepath.Join(root, "___review", "threads"))
		if err != nil {
			return err
		}
		threadDir := filepath.Join(threadsDir, id+".thread")
		return store.CommitDir(root, threadDir, func(stage string) error {
			thread := saga.ThreadManifest{Version: saga.CurrentVersion, ID: id, Target: target, Anchor: anchor, Kind: kind, CreatedAt: now}
			if kind == "suggestion" {
				thread.Suggestion = &saga.Suggestion{Replacement: replacement}
			}
			if err := store.WriteJSON(filepath.Join(stage, "thread.json"), thread, true); err != nil {
				return err
			}
			if err := injectMutationFault("after-thread-manifest"); err != nil {
				return err
			}
			_, err = addMessageToUncommittedThread(stage, body, attachments, now)
			return err
		})
	})
	if err != nil {
		return "", err
	}
	return id, nil
}

func AddDiffReview(root, uri, state string) error {
	reference, err := diffuri.Parse(uri)
	if err != nil || reference.Kind != "file" {
		return fmt.Errorf("diff review requires a valid file diff URI")
	}
	if state != "reviewed" && state != "unreviewed" {
		return fmt.Errorf("diff review requires reviewed or unreviewed state")
	}
	return mutate(root, func(index saga.MutationIndex) error {
		if err := verifySagaRepository(index, reference); err != nil {
			return err
		}
		dir, err := store.EnsureDirWithin(root, filepath.Join(root, "___review", "diffs"))
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		id := store.EventID(now)
		review := saga.DiffReview{Version: saga.CurrentVersion, ID: id, URI: uri, State: state, CreatedAt: now}
		return store.WriteJSON(filepath.Join(dir, id+"-"+state+".json"), review, true)
	})
}

func AddReply(root, threadID, body string, attachments []string) (id string, err error) {
	if strings.TrimSpace(threadID) == "" {
		return "", fmt.Errorf("thread is required")
	}
	if strings.TrimSpace(body) == "" && len(attachments) == 0 {
		return "", fmt.Errorf("message body or attachment is required")
	}
	if err := validateAttachments(attachments); err != nil {
		return "", err
	}
	now := time.Now().UTC()
	id = store.EventID(now)
	err = mutate(root, func(saga.MutationIndex) error {
		threadDir, err := existingThreadDir(root, threadID)
		if err != nil {
			return err
		}
		messagesDir, err := store.EnsureDirWithin(root, filepath.Join(threadDir, "messages"))
		if err != nil {
			return err
		}
		messageDir := filepath.Join(messagesDir, id+".message")
		return store.CommitDir(root, messageDir, func(stage string) error {
			return populateMessage(stage, id, body, attachments, now)
		})
	})
	if err != nil {
		return "", err
	}
	return id, nil
}

func SetState(root, threadID, state string) error {
	if state != "open" && state != "resolved" && state != "withdrawn" {
		return fmt.Errorf("thread state must be open, resolved, or withdrawn")
	}
	return mutate(root, func(saga.MutationIndex) error {
		threadDir, err := existingThreadDir(root, threadID)
		if err != nil {
			return err
		}
		eventsDir, err := store.EnsureDirWithin(root, filepath.Join(threadDir, "events"))
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		id := store.EventID(now)
		event := saga.ThreadEvent{Version: saga.CurrentVersion, ID: id, State: state, CreatedAt: now}
		return store.WriteJSON(filepath.Join(eventsDir, id+"-"+state+".json"), event, true)
	})
}

func SetAnchor(root, threadID string, anchor saga.Anchor) error {
	if err := saga.ValidateAnchor(anchor); err != nil {
		return err
	}
	return mutate(root, func(index saga.MutationIndex) error {
		if err := verifyAnchorRepository(index, anchor); err != nil {
			return err
		}
		threadDir, err := existingThreadDir(root, threadID)
		if err != nil {
			return err
		}
		eventsDir, err := store.EnsureDirWithin(root, filepath.Join(threadDir, "events"))
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		id := store.EventID(now)
		event := saga.ThreadEvent{Version: saga.CurrentVersion, ID: id, Anchor: &anchor, CreatedAt: now}
		return store.WriteJSON(filepath.Join(eventsDir, id+"-anchor.json"), event, true)
	})
}

func AddReview(root, targetDir, state, body string) error {
	if state != "approved" && state != "rejected" && state != "closed" && state != "open" {
		return fmt.Errorf("review requires approved, rejected, closed, or open state")
	}
	return mutate(root, func(index saga.MutationIndex) error {
		cleanTarget, err := filepath.Abs(targetDir)
		if err != nil {
			return err
		}
		known := false
		for _, dir := range index.ReviewTargets {
			if dir == cleanTarget {
				known = true
				break
			}
		}
		if !known {
			return fmt.Errorf("review target does not exist")
		}
		dir, err := store.EnsureDirWithin(root, filepath.Join(targetDir, "___approvals"))
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		id := store.EventID(now)
		review := saga.Review{Version: saga.CurrentVersion, ID: id, State: state, Body: strings.TrimSpace(body), CreatedAt: now}
		return store.WriteJSON(filepath.Join(dir, id+"-"+state+".json"), review, true)
	})
}

// verifySagaRepository refuses review records whose diff identity belongs to a
// different source repository than the saga declares. Diff URIs carry their own
// repository, base, and head, so without this check a review decision could be
// filed against a comparison this saga never describes. It runs inside the
// writer lock and before any write, so a rejected mutation leaves nothing behind.
func verifySagaRepository(index saga.MutationIndex, reference diffuri.Reference) error {
	repository, err := diffuri.CanonicalRepository(index.Manifest.Source.Repository)
	if err != nil {
		return fmt.Errorf("saga declares an invalid source repository: %w", err)
	}
	if reference.Repository != repository {
		return fmt.Errorf("diff URI repository %q does not match the saga source repository %q", reference.Repository, repository)
	}
	return nil
}

func verifyAnchorRepository(index saga.MutationIndex, anchor saga.Anchor) error {
	if anchor.Type != "diff" || anchor.Diff == nil {
		return nil
	}
	reference, err := diffuri.Parse(anchor.Diff.URI)
	if err != nil {
		return fmt.Errorf("diff anchor requires a valid diff URI: %w", err)
	}
	return verifySagaRepository(index, reference)
}

func mutate(root string, operation func(saga.MutationIndex) error) error {
	if _, err := validateMutableSaga(root); err != nil {
		return err
	}
	return store.WithSagaLock(root, store.DefaultLockTimeout, func() error {
		// Validate again under the writer lock so a concurrent supported writer or
		// external edit cannot turn a previously valid snapshot into a mutation
		// target between the check and the commit.
		index, err := validateMutableSaga(root)
		if err != nil {
			return err
		}
		return operation(index)
	})
}

func validateMutableSaga(root string) (saga.MutationIndex, error) {
	index, validation, err := saga.LoadMutationIndex(root)
	if err != nil {
		return saga.MutationIndex{}, fmt.Errorf("cannot mutate saga: %w", err)
	}
	if !validation.Valid {
		return saga.MutationIndex{}, fmt.Errorf("cannot mutate structurally invalid saga; run change-saga validate")
	}
	_, reviewValidation, err := saga.LoadReviewState(index)
	if err != nil {
		return saga.MutationIndex{}, fmt.Errorf("cannot mutate saga review state: %w", err)
	}
	if !reviewValidation.Valid {
		return saga.MutationIndex{}, fmt.Errorf("cannot mutate invalid saga review state; run change-saga validate")
	}
	return index, nil
}

func existingThreadDir(root, threadID string) (string, error) {
	// A thread is addressed by its stable id. Accepting anything else and then
	// reducing it with filepath.Base would silently retarget "../other" at a
	// different record instead of reporting an unusable identifier.
	if !saga.ValidID(threadID) {
		return "", fmt.Errorf("thread %q is not a stable identifier", threadID)
	}
	threadDir := filepath.Join(root, "___review", "threads", threadID+".thread")
	info, err := os.Lstat(filepath.Join(threadDir, "thread.json"))
	if err != nil || info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("thread %q does not exist", threadID)
	}
	if _, err := store.EnsureDirWithin(root, threadDir); err != nil {
		return "", err
	}
	return threadDir, nil
}

func addMessageToUncommittedThread(threadDir, body string, attachments []string, now time.Time) (string, error) {
	id := store.EventID(now)
	messagesDir := filepath.Join(threadDir, "messages")
	if err := os.Mkdir(messagesDir, 0o755); err != nil {
		return "", err
	}
	messageDir := filepath.Join(messagesDir, id+".message")
	if err := os.Mkdir(messageDir, 0o755); err != nil {
		return "", err
	}
	if err := populateMessage(messageDir, id, body, attachments, now); err != nil {
		return "", err
	}
	return id, nil
}

func populateMessage(messageDir, id, body string, attachments []string, now time.Time) error {
	message := saga.MessageManifest{Version: saga.CurrentVersion, ID: id, CreatedAt: now}
	if err := store.WriteJSON(filepath.Join(messageDir, "message.json"), message, true); err != nil {
		return err
	}
	if err := injectMutationFault("after-message-manifest"); err != nil {
		return err
	}
	order := 0
	if strings.TrimSpace(body) != "" {
		fragmentID := id + "-body"
		fragmentDir := filepath.Join(messageDir, "body.fragment")
		if err := os.Mkdir(fragmentDir, 0o755); err != nil {
			return err
		}
		manifest := saga.FragmentManifest{Version: saga.CurrentVersion, ID: fragmentID, MediaType: "text/markdown", Entrypoint: "content.md", Order: order}
		if err := store.WriteJSON(filepath.Join(fragmentDir, "fragment.json"), manifest, true); err != nil {
			return err
		}
		if err := store.WriteFile(filepath.Join(fragmentDir, "content.md"), []byte(body+"\n"), 0o644, true); err != nil {
			return err
		}
		order++
	}
	for i, source := range attachments {
		name := filepath.Base(source)
		if name == "fragment.json" || strings.HasPrefix(name, "___") {
			return fmt.Errorf("attachment name %q is reserved", name)
		}
		mediaType := attachmentMediaType(source)
		fragmentID := fmt.Sprintf("%s-attachment-%d", id, i+1)
		fragmentDir := filepath.Join(messageDir, fmt.Sprintf("attachment-%02d.fragment", i+1))
		if err := os.Mkdir(fragmentDir, 0o755); err != nil {
			return err
		}
		manifest := saga.FragmentManifest{Version: saga.CurrentVersion, ID: fragmentID, Title: name, MediaType: mediaType, Entrypoint: name, Order: order}
		if err := store.WriteJSON(filepath.Join(fragmentDir, "fragment.json"), manifest, true); err != nil {
			return err
		}
		data, err := os.ReadFile(source)
		if err != nil {
			return err
		}
		if err := store.WriteFile(filepath.Join(fragmentDir, name), data, 0o644, true); err != nil {
			return err
		}
		if err := injectMutationFault("after-attachment"); err != nil {
			return err
		}
		order++
	}
	return nil
}

func validateAttachments(attachments []string) error {
	for _, source := range attachments {
		info, err := os.Lstat(source)
		if err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("attachment %q must be a readable regular file", source)
		}
		mediaType := attachmentMediaType(source)
		if !supportedAttachmentType(mediaType) {
			return fmt.Errorf("unsupported attachment type %q", mediaType)
		}
	}
	return nil
}

func attachmentMediaType(path string) string {
	mediaType := mime.TypeByExtension(strings.ToLower(filepath.Ext(path)))
	if mediaType == "" {
		return "application/octet-stream"
	}
	if parsed, _, err := mime.ParseMediaType(mediaType); err == nil {
		return parsed
	}
	return mediaType
}

func supportedAttachmentType(mediaType string) bool {
	return strings.HasPrefix(mediaType, "image/") || mediaType == "text/html" || mediaType == "text/plain" || mediaType == "text/markdown"
}

func injectMutationFault(step string) error {
	if mutationFaultHook != nil {
		return mutationFaultHook(step)
	}
	return nil
}
