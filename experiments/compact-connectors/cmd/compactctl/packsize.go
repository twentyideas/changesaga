package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// runPackSize answers the only size question that actually matters to a
// repository: how large is the Git object store after committing this evidence?
//
// Raw bytes overstate the compact encoding's advantage, because Git stores
// deflated blobs and the v2 encoding's redundancy is exactly what deflate eats.
// Per-file deflate understates it in the other direction, because Git also
// deltas similar blobs against each other inside a pack. Only a real commit and
// a real `git gc` settle it.
func runPackSize(args []string) error {
	flags := flag.NewFlagSet("packsize", flag.ExitOnError)
	tree := flags.String("tree", "", "saga root whose evidence should be committed")
	extension := flags.String("extension", ".json", "evidence file extension to commit")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *tree == "" {
		return fmt.Errorf("--tree is required")
	}

	work, err := os.MkdirTemp("", "compactctl-pack-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)

	start := time.Now()
	var files int
	var raw int64
	err = filepath.WalkDir(*tree, func(current string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || filepath.Ext(entry.Name()) != *extension {
			return nil
		}
		if filepath.Base(filepath.Dir(current)) != "___diffs" {
			return nil
		}
		relative, err := filepath.Rel(*tree, current)
		if err != nil {
			return err
		}
		destination := filepath.Join(work, relative)
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			return err
		}
		in, err := os.Open(current)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.Create(destination)
		if err != nil {
			return err
		}
		written, err := io.Copy(out, in)
		if err != nil {
			out.Close()
			return err
		}
		if err := out.Close(); err != nil {
			return err
		}
		files++
		raw += written
		return nil
	})
	if err != nil {
		return err
	}
	if files == 0 {
		return fmt.Errorf("no %s evidence files found under %s", *extension, *tree)
	}
	copied := time.Since(start)

	run := func(args ...string) error {
		command := exec.CommandContext(context.Background(), "git", args...)
		command.Dir = work
		command.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=bench", "GIT_AUTHOR_EMAIL=bench@example.test",
			"GIT_COMMITTER_NAME=bench", "GIT_COMMITTER_EMAIL=bench@example.test",
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		output, err := command.CombinedOutput()
		if err != nil {
			return fmt.Errorf("git %s: %w\n%s", strings.Join(args, " "), err, output)
		}
		return nil
	}
	start = time.Now()
	for _, args := range [][]string{
		{"init", "--quiet", "--initial-branch=main", "."},
		{"add", "-A"},
		{"commit", "--quiet", "-m", "evidence"},
		{"gc", "--quiet", "--aggressive"},
	} {
		if err := run(args...); err != nil {
			return err
		}
	}
	committed := time.Since(start)

	var packed int64
	err = filepath.WalkDir(filepath.Join(work, ".git"), func(current string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		packed += info.Size()
		return nil
	})
	if err != nil {
		return err
	}

	fmt.Printf("tree           %s (%s)\n", *tree, *extension)
	fmt.Printf("files          %d\n", files)
	fmt.Printf("raw            %s\n", humanBytes(raw))
	fmt.Printf("git object store %s\n", humanBytes(packed))
	fmt.Printf("ratio          %.1fx smaller once packed\n", float64(raw)/float64(packed))
	fmt.Printf("copy %s, commit and gc %s\n", copied.Round(time.Millisecond), committed.Round(time.Millisecond))
	return nil
}
