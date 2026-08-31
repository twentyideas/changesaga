package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/twentyideas/changesaga/internal/saga"
)

// authoringScope selects a physical hierarchy root while leaving chapter,
// section, fragment, landmark, and target behavior in the shared machinery.
type authoringScope struct {
	design bool
}

var narrativeAuthoring = authoringScope{}
var designAuthoring = authoringScope{design: true}

func (scope authoringScope) command(operation string) string {
	if scope.design {
		return "design-" + operation
	}
	return operation
}

func (scope authoringScope) commandText(operation string) string {
	if scope.design {
		return "change-saga design " + operation
	}
	return "change-saga " + operation
}

func (scope authoringScope) hierarchyRoot(document *saga.Saga) (string, error) {
	if !scope.design {
		return document.Root, nil
	}
	if document.Manifest.Version != saga.CurrentSagaVersion {
		return "", fmt.Errorf("technical design authoring requires Saga format v3; run change-saga upgrade --to 3 %s", document.Root)
	}
	dir := filepath.Join(document.Root, "___design")
	info, err := os.Lstat(dir)
	if errors.Is(err, os.ErrNotExist) {
		return dir, nil
	}
	if err != nil {
		return "", err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("___design must be a real directory")
	}
	return dir, nil
}

func (scope authoringScope) resolveTarget(document *saga.Saga, value string, allowFragment bool) (string, string, error) {
	root, err := scope.hierarchyRoot(document)
	if err != nil {
		return "", "", err
	}
	if scope.design && value == "." {
		return root, "", nil
	}
	dir, target, err := resolveTarget(document, value, allowFragment)
	if scope.design && (err != nil || !pathWithin(root, dir)) && !filepath.IsAbs(value) {
		// Design paths are relative to ___design just as narrative paths are
		// relative to the Saga root. Stable IDs and URNs still resolve directly.
		dir, target, err = resolveTarget(document, filepath.Join("___design", value), allowFragment)
	}
	if err != nil {
		return "", "", err
	}
	if scope.design && !pathWithin(root, dir) {
		return "", "", fmt.Errorf("target %q is outside the technical design root", value)
	}
	return dir, target, nil
}

func pathWithin(root, candidate string) bool {
	rel, err := filepath.Rel(root, candidate)
	if err != nil || rel == ".." || filepath.IsAbs(rel) {
		return false
	}
	if len(rel) >= 3 && rel[:3] == ".."+string(filepath.Separator) {
		return false
	}
	return true
}

var designOperations = []string{"add-chapter", "add-section", "add-fragment", "set-fragment-content"}

// Design dispatches the v3 technical-design authoring family. Each operation
// calls the same implementation used by root narrative authoring with a
// different physical hierarchy root.
func Design(ctx context.Context, args []string, out io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: %s", commandUsage["design"])
	}
	if args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		printDesignHelp(out)
		return flag.ErrHelp
	}
	switch args[0] {
	case "add-chapter":
		return addChapter(ctx, args[1:], out, designAuthoring)
	case "add-section":
		return addSection(ctx, args[1:], out, designAuthoring)
	case "add-fragment":
		return addFragment(ctx, args[1:], out, designAuthoring)
	case "set-fragment-content":
		return setFragmentContentCommand(ctx, args[1:], out, os.Stdin, designAuthoring)
	default:
		return fmt.Errorf("unknown design operation %q; expected add-chapter, add-section, add-fragment, or set-fragment-content", args[0])
	}
}

func printDesignHelp(out io.Writer) {
	fmt.Fprintln(out, "Technical design authoring (Saga format v3)\n\nUsage:")
	for _, operation := range designOperations {
		fmt.Fprintf(out, "  %s\n", commandUsage["design-"+operation])
	}
}
