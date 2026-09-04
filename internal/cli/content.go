package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/twentyideas/changesaga/internal/saga"
	"github.com/twentyideas/changesaga/internal/store"
)

type fragmentContentOutput struct {
	OK        bool   `json:"ok"`
	Target    string `json:"target"`
	Path      string `json:"path"`
	MediaType string `json:"media_type"`
	Bytes     int    `json:"bytes"`
}

// SetFragmentContent replaces a fragment entrypoint without asking authors to
// depend on the on-disk package layout. stdin is bound at this boundary so the
// implementation remains deterministic in tests.
func SetFragmentContent(ctx context.Context, args []string, out io.Writer) error {
	return setFragmentContentCommand(ctx, args, out, os.Stdin, narrativeAuthoring)
}

func setFragmentContentCommand(ctx context.Context, args []string, out io.Writer, stdin io.Reader, scope authoringScope) error {
	err := setFragmentContentScoped(ctx, args, out, stdin, scope)
	if err != nil && jsonFlagRequested(args) {
		return reportJSONMutationFailure(out, err)
	}
	return err
}

func setFragmentContent(ctx context.Context, args []string, out io.Writer, stdin io.Reader) error {
	return setFragmentContentScoped(ctx, args, out, stdin, narrativeAuthoring)
}

func setFragmentContentScoped(_ context.Context, args []string, out io.Writer, stdin io.Reader, scope authoringScope) error {
	command := scope.command("set-fragment-content")
	flags := commandFlags(command, commandUsage[command], out)
	target := flags.String("target", "", "fragment path, ID, or target URN")
	source := flags.String("source", "", "content file, or - for standard input")
	jsonOutput := flags.Bool("json", false, "emit one machine-readable JSON result")
	quiet := flags.Bool("quiet", false, "suppress successful output")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 || *target == "" || *source == "" {
		return fmt.Errorf("usage: %s", commandUsage[command])
	}
	if *jsonOutput && *quiet {
		return fmt.Errorf("--json and --quiet cannot be combined")
	}
	var data []byte
	var err error
	if *source == "-" {
		if stdin == nil {
			return fmt.Errorf("--source - requires content on standard input")
		}
		data, err = io.ReadAll(stdin)
	} else {
		data, err = os.ReadFile(*source)
	}
	if err != nil {
		return fmt.Errorf("read fragment content: %w", err)
	}

	result := fragmentContentOutput{OK: true, Bytes: len(data)}
	err = authorMutation(flags.Arg(0), func(document *saga.Saga) error {
		dir, targetURN, resolveErr := scope.resolveTarget(document, *target, true)
		if resolveErr != nil {
			return resolveErr
		}
		fragment := findFragment(document.Section, dir)
		if fragment == nil || fragment.Target != targetURN {
			return fmt.Errorf("--target must identify a fragment, not a saga, chapter, section, or landmark")
		}
		if err := enforceSlideProseBudget(data, fragment.MediaType); err != nil {
			return err
		}
		entrypoint := filepath.Join(fragment.Directory, filepath.FromSlash(fragment.Entrypoint))
		if _, ensureErr := store.EnsureDirWithin(document.Root, filepath.Dir(entrypoint)); ensureErr != nil {
			return ensureErr
		}
		if info, statErr := os.Lstat(entrypoint); statErr == nil && info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("fragment entrypoint must not be a symlink")
		} else if statErr != nil && !os.IsNotExist(statErr) {
			return statErr
		}
		if writeErr := store.WriteFile(entrypoint, data, 0o644, false); writeErr != nil {
			return writeErr
		}
		result.Target = fragment.Target
		result.Path = filepath.ToSlash(filepath.Join(fragment.Path, fragment.Entrypoint))
		result.MediaType = fragment.MediaType
		return nil
	})
	if err != nil {
		return err
	}
	if *quiet {
		return nil
	}
	if *jsonOutput {
		return writeJSON(out, result)
	}
	fmt.Fprintf(out, "Updated %s (%d bytes)\n", result.Target, result.Bytes)
	return nil
}

func enforceSlideProseBudget(content []byte, mediaType string) error {
	if mediaType != "text/markdown" {
		return nil
	}
	words := saga.MarkdownSlideWordCount(string(content))
	if words <= saga.MarkdownSlideWordBudget {
		return nil
	}
	return fmt.Errorf("Markdown slide has %d prose words; split it into one-idea slides or replace prose with a visual or concrete example (maximum %d)", words, saga.MarkdownSlideWordBudget)
}
