package snapshotcache

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func testKey(saga string) Key {
	return Key{Saga: saga, Tree: "tree-a", Source: "source-a"}
}

func writeDerived(content string) func(string) error {
	return func(dir string) error {
		return os.WriteFile(filepath.Join(dir, "derived.bin"), []byte(content), 0o644)
	}
}

func readDerived(t *testing.T, dir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "derived.bin"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// TestSecondBuildReusesTheFirst is the whole point of the cache: work that
// depends only on unchanged bytes happens once.
func TestSecondBuildReusesTheFirst(t *testing.T) {
	store := testStore(t)
	key := testKey("/sagas/one")
	builds := 0
	populate := func(dir string) error {
		builds++
		return writeDerived("derived once")(dir)
	}

	first, cached, err := store.Build(key, populate)
	if err != nil {
		t.Fatal(err)
	}
	if !cached {
		t.Fatal("a valid key was not cached; every request would rebuild the whole comparison")
	}
	second, _, err := store.Build(key, populate)
	if err != nil {
		t.Fatal(err)
	}
	if builds != 1 {
		t.Fatalf("derived state was built %d times for identical inputs; want 1", builds)
	}
	if first != second {
		t.Fatalf("the same key resolved to two directories: %q and %q", first, second)
	}
	if got := readDerived(t, second); got != "derived once" {
		t.Fatalf("cached generation held %q", got)
	}
}

// TestChangedInputsMissRatherThanServeStaleState states the invalidation rule.
// There is no invalidation step to get wrong: changed inputs are a different
// address, so the old generation cannot be returned for them.
func TestChangedInputsMissRatherThanServeStaleState(t *testing.T) {
	store := testStore(t)
	saga := "/sagas/one"
	original := testKey(saga)
	if _, _, err := store.Build(original, writeDerived("before")); err != nil {
		t.Fatal(err)
	}

	for name, changed := range map[string]Key{
		"the saga's own bytes changed":  {Saga: saga, Tree: "tree-b", Source: "source-a"},
		"the source comparison changed": {Saga: saga, Tree: "tree-a", Source: "source-b"},
		"a different saga, same prints": {Saga: "/sagas/two", Tree: "tree-a", Source: "source-a"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, ok := store.Lookup(changed); ok {
				t.Fatal("a generation built from different inputs was served; a reviewer would read a saga that no longer exists")
			}
			dir, _, err := store.Build(changed, writeDerived("after"))
			if err != nil {
				t.Fatal(err)
			}
			if got := readDerived(t, dir); got != "after" {
				t.Fatalf("rebuilt generation held %q, want %q", got, "after")
			}
		})
	}

	// The superseded generation is still addressable by its own key, which is
	// what makes switching between two comparisons cheap in both directions.
	previous, ok := store.Lookup(original)
	if !ok {
		t.Fatal("the original generation disappeared when a newer one was built")
	}
	if got := readDerived(t, previous); got != "before" {
		t.Fatalf("the original generation held %q after a rebuild", got)
	}
}

// TestAFailedBuildPublishesNothing is the atomicity rule stated as a failure.
func TestAFailedBuildPublishesNothing(t *testing.T) {
	store := testStore(t)
	key := testKey("/sagas/one")
	wanted := errors.New("evaluate failed")

	_, cached, err := store.Build(key, func(dir string) error {
		// Write real content first, so what is being asserted is that a partial
		// generation is discarded rather than that nothing was written.
		if err := writeDerived("half written")(dir); err != nil {
			return err
		}
		return wanted
	})
	if !errors.Is(err, wanted) {
		t.Fatalf("Build returned %v, want %v", err, wanted)
	}
	if cached {
		t.Fatal("a failed build reported itself as cached")
	}
	if _, ok := store.Lookup(key); ok {
		t.Fatal("a failed build published a generation; the next reader would treat a partial snapshot as complete")
	}
	if state := store.State(key); state != Absent {
		t.Fatalf("state after a failed build = %v, want absent", state)
	}

	// The failure is not sticky: the same key builds normally afterwards.
	dir, cached, err := store.Build(key, writeDerived("complete"))
	if err != nil || !cached {
		t.Fatalf("rebuild after a failure: dir=%q cached=%v err=%v", dir, cached, err)
	}
	if got := readDerived(t, dir); got != "complete" {
		t.Fatalf("rebuilt generation held %q", got)
	}
}

// TestAnInterruptedBuildLeavesNoObservableGeneration checks that a reader can
// never see a directory mid-write. The staging directory is not the published
// name, so a populate that is still running is not addressable.
func TestAnInterruptedBuildLeavesNoObservableGeneration(t *testing.T) {
	store := testStore(t)
	key := testKey("/sagas/one")
	entered := make(chan struct{})
	release := make(chan struct{})

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _, _ = store.Build(key, func(dir string) error {
			if err := writeDerived("in progress")(dir); err != nil {
				return err
			}
			close(entered)
			<-release
			return errors.New("interrupted")
		})
	}()

	<-entered
	if _, ok := store.Lookup(key); ok {
		t.Fatal("a generation was readable while it was still being written")
	}
	if state := store.State(key); state != Building {
		t.Fatalf("state during a build = %v, want building; a handler could not tell a reviewer that work is under way", state)
	}
	close(release)
	wg.Wait()

	if _, ok := store.Lookup(key); ok {
		t.Fatal("an interrupted build published a generation")
	}
	// Prune removes the debris the interrupted build left behind.
	if err := store.Prune(key, 4); err != nil {
		t.Fatal(err)
	}
	assertNoStagingDebris(t, store.Root())
}

// TestConcurrentRequestsBuildOnce is what lets a burst of requests after a
// change cost one rebuild instead of one per request.
func TestConcurrentRequestsBuildOnce(t *testing.T) {
	store := testStore(t)
	key := testKey("/sagas/one")
	var builds atomic.Int64
	start := make(chan struct{})

	const callers = 16
	dirs := make([]string, callers)
	errs := make([]error, callers)
	var wg sync.WaitGroup
	for i := range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			dirs[i], _, errs[i] = store.Build(key, func(dir string) error {
				builds.Add(1)
				return writeDerived("built once")(dir)
			})
		}()
	}
	close(start)
	wg.Wait()

	if got := builds.Load(); got != 1 {
		t.Fatalf("%d concurrent requests performed %d builds; want 1", callers, got)
	}
	for i := range callers {
		if errs[i] != nil {
			t.Fatalf("caller %d: %v", i, errs[i])
		}
		if dirs[i] != dirs[0] {
			t.Fatalf("caller %d resolved to %q, caller 0 to %q", i, dirs[i], dirs[0])
		}
		if got := readDerived(t, dirs[i]); got != "built once" {
			t.Fatalf("caller %d read %q", i, got)
		}
	}
}

// TestInputsThatCannotBeDescribedExactlyAreNeverCached covers the `WORKTREE`
// head: its content is not pinned down by any cheap probe, so the server leaves
// the source fingerprint empty and nothing may be stored under it.
func TestInputsThatCannotBeDescribedExactlyAreNeverCached(t *testing.T) {
	store := testStore(t)
	for name, key := range map[string]Key{
		"a WORKTREE head leaves no source fingerprint": {Saga: "/sagas/one", Tree: "tree-a", Source: ""},
		"an unreadable saga tree":                      {Saga: "/sagas/one", Tree: "", Source: "source-a"},
		"no saga at all":                               {Saga: "", Tree: "tree-a", Source: "source-a"},
	} {
		t.Run(name, func(t *testing.T) {
			if key.Valid() {
				t.Fatal("key reported itself as valid")
			}
			var staged string
			_, cached, err := store.Build(key, func(dir string) error {
				staged = dir
				return writeDerived("uncacheable")(dir)
			})
			if err != nil {
				t.Fatal(err)
			}
			if cached {
				t.Fatal("state derived from inputs that cannot be described exactly was cached; a later request could serve it after the working tree changed")
			}
			if _, ok := store.Lookup(key); ok {
				t.Fatal("an invalid key resolved to a generation")
			}
			if state := store.State(key); state != Absent {
				t.Fatalf("state of an invalid key = %v, want absent", state)
			}
			if _, err := os.Stat(staged); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("the uncached build directory %q survived the call", staged)
			}
		})
	}
	assertNoStagingDebris(t, store.Root())
}

// TestDiscardingTheCacheChangesOnlySpeed is the disposability rule. It is the
// property that makes every other decision here reversible.
func TestDiscardingTheCacheChangesOnlySpeed(t *testing.T) {
	store := testStore(t)
	key := testKey("/sagas/one")
	populate := writeDerived("derived from the saga")

	first, _, err := store.Build(key, populate)
	if err != nil {
		t.Fatal(err)
	}
	before := readDerived(t, first)

	if err := store.Discard(key); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.Lookup(key); ok {
		t.Fatal("a discarded generation was still found")
	}

	rebuilt, cached, err := store.Build(key, populate)
	if err != nil || !cached {
		t.Fatalf("rebuild after discard: cached=%v err=%v", cached, err)
	}
	if after := readDerived(t, rebuilt); after != before {
		t.Fatalf("rebuilding after a discard produced %q, but the discarded generation held %q", after, before)
	}

	// Removing the entire cache root is equally safe.
	if err := os.RemoveAll(store.Root()); err != nil {
		t.Fatal(err)
	}
	again, _, err := store.Build(key, populate)
	if err != nil {
		t.Fatal(err)
	}
	if after := readDerived(t, again); after != before {
		t.Fatalf("rebuilding after the whole cache was deleted produced %q, want %q", after, before)
	}
}

// TestPruneBoundsGenerationsPerSaga keeps disk use bounded when a reviewer
// works through many comparisons.
func TestPruneBoundsGenerationsPerSaga(t *testing.T) {
	store := testStore(t)
	saga := "/sagas/one"
	var keys []Key
	for i := range 6 {
		key := Key{Saga: saga, Tree: "tree-a", Source: string(rune('a' + i))}
		if _, _, err := store.Build(key, writeDerived("generation "+string(rune('a'+i)))); err != nil {
			t.Fatal(err)
		}
		keys = append(keys, key)
	}

	if err := store.Prune(keys[0], 2); err != nil {
		t.Fatal(err)
	}
	bucket := filepath.Join(store.Root(), keys[0].bucket())
	entries, err := os.ReadDir(bucket)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("prune left %d generations, want 2", len(entries))
	}
	// Whichever survived must still be complete and correctly addressed; prune
	// must never leave a generation that answers to the wrong key.
	found := 0
	for _, key := range keys {
		dir, ok := store.Lookup(key)
		if !ok {
			continue
		}
		found++
		if got, want := readDerived(t, dir), "generation "+key.Source; got != want {
			t.Fatalf("surviving generation held %q, want %q", got, want)
		}
	}
	if found != 2 {
		t.Fatalf("%d of the pruned saga's keys still resolve, want 2", found)
	}

	if err := store.Prune(keys[0], 0); err == nil {
		t.Fatal("prune accepted keep=0; that silently empties the bucket where Discard is meant to be explicit")
	}
}

// TestAnotherSagaIsUnaffectedByPrune keeps one saga's churn from evicting
// another's state.
func TestAnotherSagaIsUnaffectedByPrune(t *testing.T) {
	store := testStore(t)
	mine := testKey("/sagas/mine")
	theirs := testKey("/sagas/theirs")
	if _, _, err := store.Build(mine, writeDerived("mine")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Build(theirs, writeDerived("theirs")); err != nil {
		t.Fatal(err)
	}
	if err := store.Discard(mine); err != nil {
		t.Fatal(err)
	}
	dir, ok := store.Lookup(theirs)
	if !ok {
		t.Fatal("discarding one saga's cache removed another saga's")
	}
	if got := readDerived(t, dir); got != "theirs" {
		t.Fatalf("the other saga's generation held %q", got)
	}
}

// TestGenerationsAreNeverWrittenInsideTheSaga is the merge-neutrality rule.
// Derived bytes inside a saga would be committed by accident and would conflict
// on every rebuild.
func TestGenerationsAreNeverWrittenInsideTheSaga(t *testing.T) {
	saga := t.TempDir()
	before := treeListing(t, saga)

	t.Setenv(DirEnv, filepath.Join(t.TempDir(), "cache"))
	store, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	key := Key{Saga: saga, Tree: "tree-a", Source: "source-a"}
	dir, _, err := store.Build(key, writeDerived("derived"))
	if err != nil {
		t.Fatal(err)
	}

	if relative, err := filepath.Rel(saga, dir); err == nil && !strings.HasPrefix(relative, "..") {
		t.Fatalf("a generation was written to %q, which is inside the saga at %q", dir, saga)
	}
	if relative, err := filepath.Rel(saga, store.Root()); err == nil && !strings.HasPrefix(relative, "..") {
		t.Fatalf("the cache root %q is inside the saga at %q", store.Root(), saga)
	}
	if after := treeListing(t, saga); after != before {
		t.Fatalf("building derived state changed the saga tree:\nbefore: %s\nafter:  %s", before, after)
	}
}

// TestDirEnvRedirectsTheCacheRoot keeps tests and operators off the user's real
// cache directory.
func TestDirEnvRedirectsTheCacheRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "redirected")
	t.Setenv(DirEnv, root)
	store, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	if store.Root() != root {
		t.Fatalf("cache root = %q, want %q", store.Root(), root)
	}
	if _, _, err := store.Build(testKey("/sagas/one"), writeDerived("derived")); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) == 0 {
		t.Fatalf("nothing was written to the redirected root: %d entries, err=%v", len(entries), err)
	}
}

// TestADamagedGenerationRebuildsRatherThanServingItself covers a cache
// directory that survived but lost or gained the wrong manifest. A rebuild is
// always correct, so damage must look like absence and never like a hit.
func TestADamagedGenerationRebuildsRatherThanServingItself(t *testing.T) {
	store := testStore(t)
	key := testKey("/sagas/one")
	dir, _, err := store.Build(key, writeDerived("original"))
	if err != nil {
		t.Fatal(err)
	}

	for name, damage := range map[string]func(){
		"the manifest is gone":       func() { _ = os.Remove(filepath.Join(dir, manifestName)) },
		"the manifest is unreadable": func() { _ = os.WriteFile(filepath.Join(dir, manifestName), []byte("{not json"), 0o644) },
		"the manifest names another key": func() {
			other := Key{Saga: "/sagas/other", Tree: "tree-z", Source: "source-z"}
			_ = writeJSONFile(filepath.Join(dir, manifestName), other)
		},
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := store.Build(key, writeDerived("original")); err != nil {
				t.Fatal(err)
			}
			damage()
			if _, ok := store.Lookup(key); ok {
				t.Fatal("a damaged generation reported itself as a hit")
			}
			if state := store.State(key); state != Absent {
				t.Fatalf("state of a damaged generation = %v, want absent", state)
			}
			// Missing is not enough: the damaged directory still occupies the
			// name, so a rebuild must be able to replace it. Otherwise the key is
			// wedged forever and the only repair is deleting a cache directory
			// the reviewer was never told about.
			rebuilt, cached, err := store.Build(key, writeDerived("rebuilt"))
			if err != nil {
				t.Fatalf("a damaged generation could not be rebuilt: %v", err)
			}
			if !cached {
				t.Fatal("the rebuild of a damaged generation was not cached")
			}
			if got := readDerived(t, rebuilt); got != "rebuilt" {
				t.Fatalf("rebuilt generation held %q, want %q", got, "rebuilt")
			}
			if _, ok := store.Lookup(key); !ok {
				t.Fatal("the rebuilt generation is not addressable")
			}
		})
	}
}

func writeJSONFile(path string, key Key) error {
	data := []byte(`{"Saga":"` + key.Saga + `","Tree":"` + key.Tree + `","Source":"` + key.Source + `"}`)
	return os.WriteFile(path, data, 0o644)
}

func assertNoStagingDebris(t *testing.T, root string) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if strings.HasPrefix(entry.Name(), ".change-saga-") {
			t.Fatalf("staging debris survived at %q", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func treeListing(t *testing.T, root string) string {
	t.Helper()
	var names []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		names = append(names, relative)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return strings.Join(names, "\n")
}
