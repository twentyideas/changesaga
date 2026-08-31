package cli

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/twentyideas/changesaga/internal/saga"
	"github.com/twentyideas/changesaga/internal/store"
)

var livingRootDirectories = []string{"___requirements", "___design", "___workplan"}

// upgradeFaultHook is a package-local test seam for proving that every failure
// before commit restores the original v2 tree.
var upgradeFaultHook func(string) error

func Upgrade(_ context.Context, args []string, out io.Writer) error {
	flags := commandFlags("upgrade", commandUsage["upgrade"], out)
	target := flags.Int("to", 0, "target Saga format version (must be 3)")
	jsonOutput := flags.Bool("json", false, "emit a machine-readable result")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 || !flagWasSet(flags, "to") {
		return fmt.Errorf("usage: %s", commandUsage["upgrade"])
	}
	if *target != saga.CurrentSagaVersion {
		return fmt.Errorf("--to must be %d", saga.CurrentSagaVersion)
	}

	root := flags.Arg(0)
	err := store.WithSagaLock(root, store.DefaultLockTimeout, func() error {
		document, validation, err := saga.Load(root)
		if err != nil {
			return err
		}
		if !validation.Valid {
			return fmt.Errorf("cannot upgrade an invalid Saga: %s", validationSummary(validation))
		}
		if document.Manifest.Version == saga.CurrentSagaVersion {
			return fmt.Errorf("Saga is already format v%d", saga.CurrentSagaVersion)
		}
		if document.Manifest.Version != saga.LegacySagaVersion {
			return fmt.Errorf("cannot upgrade Saga format v%d to v%d", document.Manifest.Version, saga.CurrentSagaVersion)
		}

		parent := filepath.Dir(document.Root)
		stage, err := os.MkdirTemp(parent, ".change-saga-upgrade-*.saga")
		if err != nil {
			return err
		}
		defer os.RemoveAll(stage)
		if err := os.Chmod(stage, 0o755); err != nil {
			return err
		}
		if err := copySagaForUpgrade(document.Root, stage); err != nil {
			return fmt.Errorf("stage Saga upgrade: %w", err)
		}

		upgraded := document.Manifest
		upgraded.Version = saga.CurrentSagaVersion
		upgraded.Schema = saga.V3SchemaURL
		if err := store.WriteJSON(filepath.Join(stage, "saga.json"), upgraded, false); err != nil {
			return err
		}
		for _, name := range livingRootDirectories {
			if err := os.Mkdir(filepath.Join(stage, name), 0o755); err != nil {
				return err
			}
		}
		if err := syncUpgradeTree(stage); err != nil {
			return err
		}
		if hook := upgradeFaultHook; hook != nil {
			if err := hook("after-stage"); err != nil {
				return err
			}
		}
		_, stagedValidation, err := saga.Load(stage)
		if err != nil {
			return fmt.Errorf("validate staged Saga upgrade: %w", err)
		}
		if !stagedValidation.Valid {
			return fmt.Errorf("staged Saga upgrade is invalid: %s", validationSummary(stagedValidation))
		}
		return publishSagaUpgrade(document.Root, stage)
	})
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeJSON(out, map[string]any{
			"ok": true, "operation": "upgrade", "from": saga.LegacySagaVersion, "to": saga.CurrentSagaVersion,
			"created": livingRootDirectories,
		})
	}
	fmt.Fprintf(out, "Upgraded %s from Saga format v%d to v%d\n", root, saga.LegacySagaVersion, saga.CurrentSagaVersion)
	return nil
}

func validationSummary(validation saga.Validation) string {
	var messages []string
	for _, issue := range validation.Issues {
		if issue.Severity == "error" {
			messages = append(messages, issue.Path+": "+issue.Message)
		}
	}
	if len(messages) == 0 {
		return "validation failed"
	}
	return strings.Join(messages, "; ")
}

// publishSagaUpgrade commits the already-validated staged manifest and roots
// as one rollback-safe transaction. The old manifest remains available until
// every new entry is durable, so any reported failure restores the exact v2
// manifest and removes every root created by this operation.
func publishSagaUpgrade(root, stage string) (result error) {
	backup, err := os.CreateTemp(root, ".change-saga-upgrade-manifest-*")
	if err != nil {
		return err
	}
	backupPath := backup.Name()
	if err := backup.Close(); err != nil {
		return err
	}
	if err := os.Remove(backupPath); err != nil {
		return err
	}

	created := make([]string, 0, len(livingRootDirectories))
	manifestBackedUp := false
	manifestPublished := false
	defer func() {
		if result == nil {
			return
		}
		if manifestPublished {
			_ = os.Remove(filepath.Join(root, "saga.json"))
		}
		if manifestBackedUp {
			_ = os.Rename(backupPath, filepath.Join(root, "saga.json"))
		}
		for index := len(created) - 1; index >= 0; index-- {
			_ = os.Remove(filepath.Join(root, created[index]))
		}
		_ = os.Remove(backupPath)
		_ = store.SyncDir(root)
	}()

	for _, name := range livingRootDirectories {
		if err := os.Rename(filepath.Join(stage, name), filepath.Join(root, name)); err != nil {
			return err
		}
		created = append(created, name)
		if hook := upgradeFaultHook; hook != nil {
			if err := hook("after-root:" + name); err != nil {
				return err
			}
		}
	}
	if err := os.Rename(filepath.Join(root, "saga.json"), backupPath); err != nil {
		return err
	}
	manifestBackedUp = true
	if hook := upgradeFaultHook; hook != nil {
		if err := hook("before-manifest"); err != nil {
			return err
		}
	}
	if err := os.Rename(filepath.Join(stage, "saga.json"), filepath.Join(root, "saga.json")); err != nil {
		return err
	}
	manifestPublished = true
	if hook := upgradeFaultHook; hook != nil {
		if err := hook("after-manifest"); err != nil {
			return err
		}
	}
	if err := store.SyncDir(root); err != nil {
		return err
	}
	if err := os.Remove(backupPath); err != nil {
		return err
	}
	manifestBackedUp = false
	manifestPublished = false
	// The complete v3 tree was synced before removing the private backup. A
	// cleanup sync is best effort: failing it must not report a rolled-back
	// operation after the commit point has passed.
	_ = store.SyncDir(root)
	return nil
}

func copySagaForUpgrade(source, target string) error {
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil || relative == "." {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			targetValue, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(targetValue, filepath.Join(target, relative))
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		destination := filepath.Join(target, relative)
		if info.IsDir() {
			return os.Mkdir(destination, info.Mode().Perm())
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("%s must be a regular file", filepath.ToSlash(relative))
		}
		input, err := os.Open(path)
		if err != nil {
			return err
		}
		defer input.Close()
		output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(output, input)
		if copyErr == nil {
			copyErr = output.Sync()
		}
		closeErr := output.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
}

func syncUpgradeTree(root string) error {
	var directories []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			directories = append(directories, path)
		}
		return nil
	})
	if err != nil {
		return err
	}
	// Deep paths first ensures every child entry is durable before its parent.
	sort.Slice(directories, func(i, j int) bool { return len(directories[i]) > len(directories[j]) })
	for _, directory := range directories {
		if err := store.SyncDir(directory); err != nil {
			return err
		}
	}
	return nil
}
