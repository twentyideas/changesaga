package reviewstore

import (
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/change-saga/change-saga/internal/diffuri"
	"github.com/change-saga/change-saga/internal/saga"
	"github.com/change-saga/change-saga/internal/store"
)

func AddThread(root, target, body string, anchor saga.Anchor, kind, replacement string, attachments []string) (string, error) {
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
	id := store.EventID(now)
	threadsDir, err := store.EnsureDirWithin(root, filepath.Join(root, "___review", "threads"))
	if err != nil {
		return "", err
	}
	threadDir := filepath.Join(threadsDir, id+".thread")
	if err := os.Mkdir(threadDir, 0o755); err != nil {
		return "", err
	}
	thread := saga.ThreadManifest{Version: saga.CurrentVersion, ID: id, Target: target, Anchor: anchor, Kind: kind, CreatedAt: now}
	if kind == "suggestion" {
		thread.Suggestion = &saga.Suggestion{Replacement: replacement}
	}
	if err := store.WriteJSON(filepath.Join(threadDir, "thread.json"), thread, true); err != nil {
		return "", err
	}
	if _, err := addMessage(threadDir, body, attachments, now); err != nil {
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
	dir, err := store.EnsureDirWithin(root, filepath.Join(root, "___review", "diffs"))
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	id := store.EventID(now)
	review := saga.DiffReview{Version: saga.CurrentVersion, ID: id, URI: uri, State: state, CreatedAt: now}
	return store.WriteJSON(filepath.Join(dir, id+"-"+state+".json"), review, true)
}

func AddReply(root, threadID, body string, attachments []string) (string, error) {
	if strings.TrimSpace(threadID) == "" {
		return "", fmt.Errorf("thread is required")
	}
	if err := validateAttachments(attachments); err != nil {
		return "", err
	}
	threadDir := filepath.Join(root, "___review", "threads", filepath.Base(threadID)+".thread")
	if info, err := os.Stat(filepath.Join(threadDir, "thread.json")); err != nil || info.IsDir() {
		return "", fmt.Errorf("thread %q does not exist", threadID)
	}
	if _, err := store.EnsureDirWithin(root, threadDir); err != nil {
		return "", err
	}
	return addMessage(threadDir, body, attachments, time.Now().UTC())
}

func SetState(root, threadID, state string) error {
	if state != "open" && state != "resolved" && state != "withdrawn" {
		return fmt.Errorf("thread state must be open, resolved, or withdrawn")
	}
	threadDir := filepath.Join(root, "___review", "threads", filepath.Base(threadID)+".thread")
	if info, err := os.Stat(filepath.Join(threadDir, "thread.json")); err != nil || info.IsDir() {
		return fmt.Errorf("thread %q does not exist", threadID)
	}
	eventsDir, err := store.EnsureDirWithin(root, filepath.Join(threadDir, "events"))
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	id := store.EventID(now)
	event := saga.ThreadEvent{Version: saga.CurrentVersion, ID: id, State: state, CreatedAt: now}
	return store.WriteJSON(filepath.Join(eventsDir, id+"-"+state+".json"), event, true)
}

func SetAnchor(root, threadID string, anchor saga.Anchor) error {
	if err := saga.ValidateAnchor(anchor); err != nil {
		return err
	}
	threadDir := filepath.Join(root, "___review", "threads", filepath.Base(threadID)+".thread")
	if info, err := os.Stat(filepath.Join(threadDir, "thread.json")); err != nil || info.IsDir() {
		return fmt.Errorf("thread %q does not exist", threadID)
	}
	eventsDir, err := store.EnsureDirWithin(root, filepath.Join(threadDir, "events"))
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	id := store.EventID(now)
	event := saga.ThreadEvent{Version: saga.CurrentVersion, ID: id, Anchor: &anchor, CreatedAt: now}
	return store.WriteJSON(filepath.Join(eventsDir, id+"-anchor.json"), event, true)
}

func AddReview(root, targetDir, state, body string) error {
	if state != "approved" && state != "rejected" && state != "closed" && state != "open" {
		return fmt.Errorf("review requires approved, rejected, closed, or open state")
	}
	dir, err := store.EnsureDirWithin(root, filepath.Join(targetDir, "___approvals"))
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	id := store.EventID(now)
	review := saga.Review{Version: saga.CurrentVersion, ID: id, State: state, Body: strings.TrimSpace(body), CreatedAt: now}
	return store.WriteJSON(filepath.Join(dir, id+"-"+state+".json"), review, true)
}

func addMessage(threadDir, body string, attachments []string, now time.Time) (string, error) {
	if strings.TrimSpace(body) == "" && len(attachments) == 0 {
		return "", fmt.Errorf("message body or attachment is required")
	}
	id := store.EventID(now)
	messagesDir := filepath.Join(threadDir, "messages")
	if err := os.MkdirAll(messagesDir, 0o755); err != nil {
		return "", err
	}
	messageDir := filepath.Join(messagesDir, id+".message")
	if err := os.Mkdir(messageDir, 0o755); err != nil {
		return "", err
	}
	message := saga.MessageManifest{Version: saga.CurrentVersion, ID: id, CreatedAt: now}
	if err := store.WriteJSON(filepath.Join(messageDir, "message.json"), message, true); err != nil {
		return "", err
	}
	order := 0
	if strings.TrimSpace(body) != "" {
		fragmentID := id + "-body"
		fragmentDir := filepath.Join(messageDir, "body.fragment")
		if err := os.Mkdir(fragmentDir, 0o755); err != nil {
			return "", err
		}
		manifest := saga.FragmentManifest{Version: saga.CurrentVersion, ID: fragmentID, MediaType: "text/markdown", Entrypoint: "content.md", Order: order}
		if err := store.WriteJSON(filepath.Join(fragmentDir, "fragment.json"), manifest, true); err != nil {
			return "", err
		}
		if err := os.WriteFile(filepath.Join(fragmentDir, "content.md"), []byte(body+"\n"), 0o644); err != nil {
			return "", err
		}
		order++
	}
	for i, source := range attachments {
		name := filepath.Base(source)
		mediaType := mime.TypeByExtension(strings.ToLower(filepath.Ext(name)))
		if mediaType == "" {
			mediaType = "application/octet-stream"
		} else if parsed, _, err := mime.ParseMediaType(mediaType); err == nil {
			mediaType = parsed
		}
		if !strings.HasPrefix(mediaType, "image/") && mediaType != "text/html" && mediaType != "text/plain" && mediaType != "text/markdown" {
			return "", fmt.Errorf("unsupported attachment type %q", mediaType)
		}
		fragmentID := fmt.Sprintf("%s-attachment-%d", id, i+1)
		fragmentDir := filepath.Join(messageDir, fmt.Sprintf("attachment-%02d.fragment", i+1))
		if err := os.Mkdir(fragmentDir, 0o755); err != nil {
			return "", err
		}
		manifest := saga.FragmentManifest{Version: saga.CurrentVersion, ID: fragmentID, Title: name, MediaType: mediaType, Entrypoint: name, Order: order}
		if err := store.WriteJSON(filepath.Join(fragmentDir, "fragment.json"), manifest, true); err != nil {
			return "", err
		}
		data, err := os.ReadFile(source)
		if err != nil {
			return "", err
		}
		if err := os.WriteFile(filepath.Join(fragmentDir, name), data, 0o644); err != nil {
			return "", err
		}
		order++
	}
	return id, nil
}

func validateAttachments(attachments []string) error {
	for _, source := range attachments {
		info, err := os.Stat(source)
		if err != nil || info.IsDir() {
			return fmt.Errorf("attachment %q must be a readable file", source)
		}
		mediaType := mime.TypeByExtension(strings.ToLower(filepath.Ext(source)))
		if parsed, _, err := mime.ParseMediaType(mediaType); err == nil {
			mediaType = parsed
		}
		if !strings.HasPrefix(mediaType, "image/") && mediaType != "text/html" && mediaType != "text/plain" && mediaType != "text/markdown" {
			return fmt.Errorf("unsupported attachment type %q", mediaType)
		}
	}
	return nil
}
