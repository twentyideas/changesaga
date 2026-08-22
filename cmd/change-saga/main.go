package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"

	"github.com/twentyideas/changesaga/internal/cli"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		cli.PrintHelp(stdout)
		return 0
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	var err error
	switch args[0] {
	case "help", "-h", "--help":
		cli.PrintHelp(stdout)
		return 0
	case "version", "--version":
		fmt.Fprintln(stdout, cli.VersionString())
		return 0
	case "init":
		err = cli.Init(ctx, args[1:], stdout)
	case "add-section":
		err = cli.AddSection(ctx, args[1:], stdout)
	case "add-chapter":
		err = cli.AddChapter(ctx, args[1:], stdout)
	case "add-fragment":
		err = cli.AddFragment(ctx, args[1:], stdout)
	case "set-fragment-content":
		err = cli.SetFragmentContent(ctx, args[1:], stdout)
	case "add-landmark":
		err = cli.AddLandmark(ctx, args[1:], stdout)
	case "cover":
		err = cli.Cover(ctx, args[1:], stdout)
	case "remove-coverage":
		err = cli.RemoveCoverage(ctx, args[1:], stdout)
	case "replace-coverage":
		err = cli.ReplaceCoverage(ctx, args[1:], stdout)
	case "add-claim":
		err = cli.AddClaim(ctx, args[1:], stdout)
	case "verify-claim":
		err = cli.VerifyClaim(ctx, args[1:], stdout)
	case "thread":
		err = cli.Thread(ctx, args[1:], stdout)
	case "reply":
		err = cli.Reply(ctx, args[1:], stdout)
	case "review":
		err = cli.Review(ctx, args[1:], stdout)
	case "validate":
		err = cli.Validate(ctx, args[1:], stdout)
	case "status", "check":
		err = cli.Status(ctx, args[1:], stdout)
	case "query":
		err = cli.Query(ctx, args[1:], stdout)
	case "serve", "open":
		err = cli.Serve(ctx, args[1:], stdout, args[0] == "open")
	case "install-skill":
		err = cli.InstallSkill(args[1:], stdout)
	case "spec":
		err = cli.Spec(args[1:], stdout)
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n", args[0])
		cli.PrintHelp(stderr)
		return 2
	}

	if err == nil {
		return 0
	}
	var statusErr *cli.StatusError
	if errors.As(err, &statusErr) {
		return statusErr.Code
	}
	if errors.Is(err, flag.ErrHelp) || strings.TrimSpace(err.Error()) == "flag: help requested" {
		return 0
	}
	var jsonErr *json.SyntaxError
	if errors.As(err, &jsonErr) {
		fmt.Fprintf(stderr, "invalid JSON: %v\n", err)
	} else {
		fmt.Fprintf(stderr, "error: %v\n", err)
	}
	return 1
}
