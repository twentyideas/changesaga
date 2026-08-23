// Package reader loads a saga whose coverage evidence is encoded as connector
// shards and hands it to the unmodified coverage evaluator.
//
// The decisive design choice here is that nothing in this package decides
// ownership, overlap, or staleness. It decodes shards into the same
// saga.DiffFile values a v2 evidence file produces and lets
// coverage.Evaluate reach its own verdict, so connector evidence cannot drift
// from v2 evidence in what it means.
package reader

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/twentyideas/changesaga/experiments/compact-connectors/connector"
	"github.com/twentyideas/changesaga/experiments/compact-connectors/migrate"
	"github.com/twentyideas/changesaga/internal/saga"
)

// Shard is one decoded connector file together with where it came from.
type Shard struct {
	// Path is the saga-root-relative path of the connector file.
	Path string
	// Owner is the target URN that owns the shard, taken from the directory.
	Owner string
	File  connector.File
	// Size and ModTimeNanos are the cheap staleness probe the derived index
	// uses; they are recorded here so the index never re-reads a shard it
	// already knows.
	Size         int64
	ModTimeNanos int64
}

// Load reads a saga and replaces each owner's evidence with the evidence its
// connector shards describe. Shards are read at the requested granularity:
// Exact expands a range back into one reference per line and reproduces v2
// selector identity exactly, Ranges keeps the compact selector.
//
// Existing v2 JSON evidence in the same directory is discarded when a shard is
// present for that owner, because a dual-encoded saga states the same atoms
// twice and counting both would manufacture an overlap that does not exist.
func Load(root string, granularity connector.Granularity) (*saga.Saga, saga.Validation, []Shard, error) {
	document, validation, err := saga.Load(root)
	if err != nil {
		return nil, validation, nil, err
	}
	shards, err := ReadShards(root)
	if err != nil {
		return nil, validation, nil, err
	}
	byOwnerDir := map[string][]Shard{}
	for _, shard := range shards {
		byOwnerDir[ownerDirOf(shard.Path)] = append(byOwnerDir[ownerDirOf(shard.Path)], shard)
	}
	for _, owner := range migrate.Owners(document) {
		owned := byOwnerDir[owner.Dir]
		if len(owned) == 0 {
			continue
		}
		files := make([]saga.DiffFile, 0, len(owned))
		for _, shard := range owned {
			references, err := shard.File.References(granularity)
			if err != nil {
				return nil, validation, nil, err
			}
			files = append(files, saga.DiffFile{Path: shard.Path, Version: saga.CurrentVersion, Diffs: references})
		}
		sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
		owner.Attach(files)
	}
	return document, validation, shards, nil
}

// ReadShards walks a saga root and decodes every connector shard it contains.
func ReadShards(root string) ([]Shard, error) {
	var shards []Shard
	err := filepath.WalkDir(root, func(current string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || filepath.Ext(entry.Name()) != connector.Extension {
			return nil
		}
		if filepath.Base(filepath.Dir(current)) != connector.EvidenceDirectory {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		file, err := migrate.ReadShard(current)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, current)
		if err != nil {
			return err
		}
		shards = append(shards, Shard{
			Path: filepath.ToSlash(rel), Owner: file.Owner, File: file,
			Size: info.Size(), ModTimeNanos: info.ModTime().UnixNano(),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(shards, func(i, j int) bool { return shards[i].Path < shards[j].Path })
	return shards, nil
}

// Stat lists the shards of a saga without decoding them. The derived index
// uses it to decide what changed before paying to parse anything.
func Stat(root string) ([]Shard, error) {
	var shards []Shard
	err := filepath.WalkDir(root, func(current string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || filepath.Ext(entry.Name()) != connector.Extension {
			return nil
		}
		if filepath.Base(filepath.Dir(current)) != connector.EvidenceDirectory {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, current)
		if err != nil {
			return err
		}
		shards = append(shards, Shard{
			Path: filepath.ToSlash(rel), Size: info.Size(), ModTimeNanos: info.ModTime().UnixNano(),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(shards, func(i, j int) bool { return shards[i].Path < shards[j].Path })
	return shards, nil
}

// ReadShardAt decodes one shard by its saga-root-relative path.
func ReadShardAt(root, relative string) (Shard, error) {
	current := filepath.Join(root, filepath.FromSlash(relative))
	info, err := os.Stat(current)
	if err != nil {
		return Shard{}, err
	}
	file, err := migrate.ReadShard(current)
	if err != nil {
		return Shard{}, err
	}
	return Shard{
		Path: relative, Owner: file.Owner, File: file,
		Size: info.Size(), ModTimeNanos: info.ModTime().UnixNano(),
	}, nil
}

// ownerDirOf strips the "___diffs/<name>.connectors" tail from a shard path,
// leaving the owning package directory. The saga root's own evidence yields an
// empty directory.
func ownerDirOf(shardPath string) string {
	dir := path.Dir(shardPath)
	dir = strings.TrimSuffix(dir, connector.EvidenceDirectory)
	return strings.TrimSuffix(dir, "/")
}
