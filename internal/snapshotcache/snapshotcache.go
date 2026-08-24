// Package snapshotcache stores derived saga state outside every saga.
//
// Building the state a reviewer request needs — the loaded document, the Git
// comparison, and the coverage report over it — costs a saga load, two `git
// diff` invocations, and a full coverage evaluation. None of that changes while
// the bytes it was built from do not, so it is worth keeping. Keeping it is
// only safe under four rules, and this package exists to hold all four in one
// place rather than spread across the handlers that benefit from them.
//
// **The key is the fingerprint.** A generation is addressed by the exact inputs
// it was derived from, so there is no invalidation step that could be forgotten
// or run late. Changed inputs produce a different address, which misses. A miss
// is always safe; a hit can never be stale, because a hit means the inputs are
// byte-identical to the ones that produced it. Inputs that cannot be described
// exactly — a `WORKTREE` head, whose content no cheap probe pins down — make
// the key invalid, and an invalid key is never stored and never found.
//
// **Creation is atomic.** A generation is populated in a staging directory and
// published with a single rename, so a reader either sees a complete generation
// or sees nothing. A failed or abandoned build leaves no directory behind, and
// a reader never observes one being written.
//
// **The cache is disposable.** Deleting any part of it, at any time, changes
// only how long the next answer takes and never what the answer is. Nothing is
// stored here that cannot be rebuilt from the saga and the source checkout.
//
// **The cache is never inside a saga.** A saga is a Git-native directory that
// people merge. Derived bytes in it would be committed by accident, would
// conflict on every rebuild, and would be an opaque binary artifact in a format
// built to be reviewed as text. Generations live under the user's cache
// directory, keyed by the saga's absolute path, so the saga tree is untouched
// and merge behaviour is unchanged.
//
// This package deliberately stores directories rather than a database. What a
// request needs is a small, named part of the derived state — the atoms for one
// file, the rollup for one target — and a directory of independently readable
// parts serves that with one os.ReadFile, keeping resident memory proportional
// to what was asked for rather than to the size of the comparison. A query
// engine would add a dependency, a second durability model, and lock and
// journal files that must be kept out of the saga, and would buy nothing that
// addressing and partitioning do not already give.
package snapshotcache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/twentyideas/changesaga/internal/store"
)

// DirEnv redirects the cache root. Tests set it so they never touch the user's
// real cache, and it gives an operator a way to place generations on a
// different filesystem.
const DirEnv = "CHANGE_SAGA_CACHE_DIR"

// manifestName records which key a generation belongs to. It is written inside
// the staging directory before the rename, so every published generation has at
// least one entry and can never be an empty directory that a concurrent rename
// would be allowed to replace.
const manifestName = "___index.json"

// Key addresses one generation of derived state.
//
// Saga is the absolute path of the saga the state describes; it separates
// unrelated sagas that would otherwise share a cache. Tree and Source are the
// two fingerprints the reviewer server already computes on every request: the
// saga's own bytes, and the identity of the exact source comparison. Either one
// empty means the input could not be described exactly, which makes the whole
// key invalid rather than risking a stale review.
type Key struct {
	Saga   string
	Tree   string
	Source string
}

// Valid reports whether the key describes its inputs exactly enough to cache.
// A `WORKTREE` head produces an empty Source and is correctly refused here.
func (k Key) Valid() bool {
	return strings.TrimSpace(k.Saga) != "" &&
		strings.TrimSpace(k.Tree) != "" &&
		strings.TrimSpace(k.Source) != ""
}

// bucket is the per-saga directory. It depends only on the saga's identity, so
// every generation of one saga prunes together.
func (k Key) bucket() string {
	return digest("saga\x00" + k.Saga)
}

// generation is the per-input directory. Both fingerprints and the saga path
// are committed to, so no two distinct input sets can share an address.
func (k Key) generation() string {
	return digest("inputs\x00" + k.Saga + "\x00" + k.Tree + "\x00" + k.Source)
}

func digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:16])
}

// State is what a handler needs in order to answer a request that arrives while
// the derived state does not yet exist.
type State int

const (
	// Absent means nothing is cached and nobody is building it.
	Absent State = iota
	// Building means another request is building this exact generation. A
	// handler can answer with progress instead of blocking or starting a second
	// build of the same work.
	Building
	// Ready means a complete generation is on disk.
	Ready
)

func (s State) String() string {
	switch s {
	case Building:
		return "building"
	case Ready:
		return "ready"
	default:
		return "absent"
	}
}

// Store is a rooted collection of generations.
type Store struct {
	root string

	// inflight serializes builds of the same generation within this process and
	// makes them observable as Building. The reviewer server is a single
	// loopback process, so this covers the case that actually occurs: a burst of
	// requests arriving while the first one is still building.
	mutex    sync.Mutex
	inflight map[string]*build
}

type build struct {
	done chan struct{}
	dir  string
	err  error
}

// Open roots a store at an explicit directory. The directory is created if it
// does not exist.
func Open(root string) (*Store, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("snapshot cache root is required")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(abs, 0o700); err != nil {
		return nil, err
	}
	// Generations describe a reviewer's private work; keep them unreadable to
	// other users even if the cache directory already existed with wider bits.
	_ = os.Chmod(abs, 0o700)
	return &Store{root: abs, inflight: map[string]*build{}}, nil
}

// Default roots a store under the user's cache directory, or under DirEnv when
// it is set. It follows the placement the CLI already uses for detached server
// state, which keeps every derived artifact this tool writes in one predictable
// place outside every saga.
func Default() (*Store, error) {
	if override := os.Getenv(DirEnv); strings.TrimSpace(override) != "" {
		return Open(override)
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return nil, err
	}
	return Open(filepath.Join(cache, "change-saga", "snapshot-index"))
}

// Root is the directory generations are written under. It is reported so a
// caller can assert what this package guarantees: that it is not inside any
// saga.
func (s *Store) Root() string { return s.root }

// Lookup returns the directory of a complete generation for the key. A miss is
// reported for an invalid key, for a generation that was never built, and for
// one whose manifest does not name this key — the last of which means the
// directory is damaged rather than merely absent, and is treated the same way,
// because a rebuild is always correct.
func (s *Store) Lookup(key Key) (string, bool) {
	if !key.Valid() {
		return "", false
	}
	dir := s.dir(key)
	data, err := os.ReadFile(filepath.Join(dir, manifestName))
	if err != nil {
		return "", false
	}
	var recorded Key
	if err := json.Unmarshal(data, &recorded); err != nil {
		return "", false
	}
	if recorded != key {
		return "", false
	}
	return dir, true
}

// State reports whether the generation is ready, being built by this process,
// or absent. An invalid key is always Absent: it is never stored, so it can
// never become ready.
func (s *Store) State(key Key) State {
	if !key.Valid() {
		return Absent
	}
	if _, ok := s.Lookup(key); ok {
		return Ready
	}
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if _, ok := s.inflight[key.generation()]; ok {
		return Building
	}
	return Absent
}

// Build returns the directory of a complete generation, creating it if needed.
//
// populate receives an empty staging directory and writes the derived state
// into it. It runs at most once per generation per process: concurrent callers
// for the same key wait for the first one and share its result, so a burst of
// requests after a change performs the work once rather than once per request.
// If populate returns an error, nothing is published and the error is returned
// to every waiter.
//
// An invalid key builds into a caller-owned temporary directory that is removed
// before returning, so an uncacheable saga still gets its state without leaving
// anything behind. The returned directory is empty in that case and the boolean
// reports that the result was not cached.
func (s *Store) Build(key Key, populate func(dir string) error) (string, bool, error) {
	if populate == nil {
		return "", false, errors.New("populate is required")
	}
	if !key.Valid() {
		return "", false, s.buildUncached(populate)
	}
	if dir, ok := s.Lookup(key); ok {
		return dir, true, nil
	}

	s.mutex.Lock()
	if existing, ok := s.inflight[key.generation()]; ok {
		s.mutex.Unlock()
		<-existing.done
		return existing.dir, existing.err == nil, existing.err
	}
	current := &build{done: make(chan struct{})}
	s.inflight[key.generation()] = current
	s.mutex.Unlock()

	current.dir, current.err = s.create(key, populate)
	close(current.done)

	s.mutex.Lock()
	delete(s.inflight, key.generation())
	s.mutex.Unlock()

	return current.dir, current.err == nil, current.err
}

// buildUncached runs populate against a directory that is removed on every
// return path. It exists so an uncacheable key is a placement decision rather
// than a separate code path in every caller.
func (s *Store) buildUncached(populate func(dir string) error) error {
	stage, err := os.MkdirTemp(s.root, ".change-saga-uncached-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(stage) }()
	return populate(stage)
}

// create publishes one generation with a single rename.
//
// The staging directory is a sibling of the destination, so the rename is
// within one filesystem and is atomic. A reader therefore observes either no
// generation or a complete one, and an interrupted build leaves only a staging
// directory that the next prune removes.
func (s *Store) create(key Key, populate func(dir string) error) (string, error) {
	final := s.dir(key)
	bucket := filepath.Dir(final)
	if err := os.MkdirAll(bucket, 0o700); err != nil {
		return "", err
	}
	stage, err := os.MkdirTemp(bucket, ".change-saga-stage-")
	if err != nil {
		return "", err
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.RemoveAll(stage)
		}
	}()
	if err := populate(stage); err != nil {
		return "", err
	}
	// The manifest is written last and inside the stage, so it is published by
	// the same rename as the content it describes. Its presence is what makes a
	// directory a generation rather than debris.
	if err := store.WriteJSON(filepath.Join(stage, manifestName), key, false); err != nil {
		return "", err
	}
	if err := syncTree(stage); err != nil {
		return "", err
	}
	if err := s.publish(key, stage, final); err != nil {
		return "", err
	}
	committed = true
	if err := store.SyncDir(bucket); err != nil {
		return "", err
	}
	return final, nil
}

// publish renames a populated stage onto its final name, resolving the two
// ways that name can already be taken.
//
// A complete generation there was published by another process from the same
// inputs, so it is as correct as the one being discarded. Anything else is
// damage — an interrupted rename, a truncated manifest, a directory a user
// copied in — and damage must not be able to wedge a generation permanently
// unbuildable, because the only repair a reviewer would have is deleting a
// cache directory they were never told about. Damage is removed and the rename
// retried once; a second failure is reported rather than retried again, so a
// destination that keeps reappearing cannot spin here.
func (s *Store) publish(key Key, stage, final string) error {
	err := os.Rename(stage, final)
	if err == nil {
		return nil
	}
	if _, ok := s.Lookup(key); ok {
		return nil
	}
	if _, statErr := os.Lstat(final); statErr != nil {
		// The name is not taken, so the rename failed for some other reason and
		// that reason is what the caller needs to hear.
		return err
	}
	if removeErr := os.RemoveAll(final); removeErr != nil {
		return fmt.Errorf("replace damaged cache generation: %w", removeErr)
	}
	if retryErr := os.Rename(stage, final); retryErr != nil {
		// A concurrent writer may have taken the name between the removal and
		// the retry. Its generation is built from the same inputs, so it stands.
		if _, ok := s.Lookup(key); ok {
			return nil
		}
		return retryErr
	}
	return nil
}

func (s *Store) dir(key Key) string {
	return filepath.Join(s.root, key.bucket(), key.generation())
}

// Prune keeps the newest generations of one saga and removes the rest, along
// with any staging directory an interrupted build left behind. Generations are
// ordered by modification time, so the ones a reviewer is switching between
// survive and the ones superseded long ago do not.
//
// keep below one is refused rather than silently emptying the bucket: removing
// every generation is what Discard is for, and a caller that computed keep from
// a configuration value should hear about a zero.
func (s *Store) Prune(key Key, keep int) error {
	if keep < 1 {
		return fmt.Errorf("keep must be at least 1, got %d", keep)
	}
	bucket := filepath.Join(s.root, key.bucket())
	entries, err := os.ReadDir(bucket)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	type generation struct {
		name    string
		modTime int64
	}
	var live []generation
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if strings.HasPrefix(entry.Name(), ".change-saga-") {
			// Debris from a build that was interrupted before its rename.
			_ = os.RemoveAll(filepath.Join(bucket, entry.Name()))
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		live = append(live, generation{name: entry.Name(), modTime: info.ModTime().UnixNano()})
	}
	// Newest first, with the name as a tiebreak so equal timestamps — which a
	// coarse filesystem clock produces readily — prune deterministically.
	sort.Slice(live, func(i, j int) bool {
		if live[i].modTime != live[j].modTime {
			return live[i].modTime > live[j].modTime
		}
		return live[i].name < live[j].name
	})
	for _, extra := range live[min(keep, len(live)):] {
		if err := os.RemoveAll(filepath.Join(bucket, extra.name)); err != nil {
			return err
		}
	}
	return store.SyncDir(bucket)
}

// Discard removes every generation of one saga. It is always safe: the next
// request rebuilds, and rebuilding is what produced these bytes in the first
// place.
func (s *Store) Discard(key Key) error {
	if err := os.RemoveAll(filepath.Join(s.root, key.bucket())); err != nil {
		return err
	}
	return nil
}

func syncTree(root string) error {
	var dirs []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			dirs = append(dirs, path)
		}
		return nil
	})
	if err != nil {
		return err
	}
	for i := len(dirs) - 1; i >= 0; i-- {
		if err := store.SyncDir(dirs[i]); err != nil {
			return err
		}
	}
	return nil
}
