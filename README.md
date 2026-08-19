# Review Saga

Review Saga is an experimental, Git-native way to review changes that are too
large to understand as one flat pull-request diff. A saga reorganizes a PR into
a big-picture overview and independently reviewable chapters—roughly the PRs
that might have existed if the change were split. Sections and fragments recurse
inside each chapter, and the tool proves that every changed line appears
somewhere in that story.

The saga is a `*.saga/` directory made from fragments: Markdown, SVG, images,
plain text, or fully interactive HTML packages with JavaScript. It may live
beside the code or in a separate repository. Everything—including drawings,
text highlights, image replies, and review state—is diffable, branchable, and
attributable through Git.

> [!WARNING]
> This repository is an early scaffold and the v2 format is experimental. It is
> ready for local trials, not yet a compatibility promise.

## What works

- Recursive sections containing stable, directory-backed fragments.
- First-class chapters with independent diff coverage, discussions, and approval state.
- Sandboxed interactive HTML and SVG fragments with bundled JavaScript/assets.
- Exact accounting of added and deleted lines from a Git comparison.
- File-level accounting for renames, mode changes, and binary changes.
- Absolute diff URIs that remain unambiguous across repositories.
- Strict structural validation and machine-readable status output for AI tools.
- Separation of product changes from changes inside any `*.saga/` directory.
- A local reviewer with rectangle/freehand overlays, text highlights, threaded
  replies, fragment attachments, and append-only resolve/reopen events.
- A Saga view with per-fragment diff drawers and a full Code Diff view with a
  changed-file tree, line comments, suggestions, and reviewed-file tracking.
- Consistent saga-, section-, and fragment-level approvals.

## Install

Go 1.26 or newer is currently used by the scaffold:

```sh
go build -o ./bin/saga ./cmd/saga
```

After the project is published at its provisional module path, installation can
use `go install github.com/review-saga/review-saga/cmd/saga@latest`.

During local development:

```sh
go run ./cmd/saga help
```

## A first saga

Create a saga for a branch compared with the merge base of `main` and `HEAD`:

```sh
saga init --base main --head HEAD --title "Checkout rewrite" pr-1234.saga
saga add-chapter --title "Backend behavior" pr-1234.saga backend
saga add-section --title "Request flow" pr-1234.saga backend.chapter/request-flow
saga add-fragment --section backend.chapter/request-flow --type html \
  --title "Try the request flow" pr-1234.saga
saga status --json pr-1234.saga
```

To import a complete interactive package, pass its directory; `index.html` is
the default HTML entrypoint and can be overridden with `--entrypoint`:

```sh
saga add-fragment --section backend.chapter/request-flow --type html \
  --source ./review-demos/request-flow --entrypoint index.html pr-1234.saga
```

The JSON status lists every uncovered diff atom. An author or AI agent can group
those atoms into coherent sections and attach evidence:

```sh
saga cover \
  --target backend.chapter/request-flow/try-the-request-flow.fragment \
  --path internal/checkout/handler.go \
  --side new \
  --lines 18-42,57-63 \
  --note "HTTP entry point and validation" \
  pr-1234.saga
```

Deleted lines use `--side old`. A pure file event is covered with, for example,
`--event rename --old-path old.go --new-path new.go`.

Each resulting evidence record contains a `saga-diff://v1/...` URI with the
absolute source repository URI and resolved base/head identities. For a saga in
a separate repository, add `--repo /path/to/source-checkout` to `cover`,
`status`, and `open`.

Continue until the check succeeds, then open the local review:

```sh
saga status pr-1234.saga
saga open pr-1234.saga
```

`status` exits with code 3 while coverage is incomplete, which makes it suitable
for an authoring loop or CI. `validate` checks only the on-disk structure and
exits with code 1 for schema errors.

## AI-guided authoring

The repository includes a distributable
[`review-saga` skill](skills/review-saga/SKILL.md). It directs an AI coding agent
to use uncovered atoms as a work queue, understand the diff before grouping it,
prefer reviewer-oriented flows over file-type buckets, reconcile stale evidence,
and resist reaching 100% with unjustifiably broad selectors.

## Directory shape

```text
pr-1234.saga/
├── saga.json
├── ___diffs/
├── ___approvals/
├── ___review/
│   ├── diffs/
│   │   └── review-event.json
│   └── threads/
│       └── thread-id.thread/
│           ├── thread.json
│           ├── events/
│           └── messages/
├── overview.fragment/
│   ├── fragment.json
│   └── content.md
└── backend.chapter/
    ├── chapter.json
    ├── overview.fragment/
    └── request-flow/
        ├── section.json
        └── interactive-demo.fragment/
            ├── fragment.json
            ├── index.html
            ├── app.js
            └── ___diffs/
```

Every root `.chapter` directory is an independent review boundary, every
ordinary nested directory is a section, and every `.fragment` directory is an
atomic content package. Review threads live in a separate overlay and their
messages contain fragments too, so comments can be Markdown, images, SVG, or
interactive HTML rather than one text field.

The local reviewer has two modes. **Saga** keeps the authored narrative in the
foreground and opens attached code in a scrollable right-hand drawer. **Code
Diff** presents the entire comparison with a changed-file tree. Both modes use
the same diff rows and thread records, so a comment or suggestion made in one is
visible in the other. Reviewed-file markers and fragment approvals are
append-only metadata committed with the saga.

Review data is intentionally conflict-resistant: separate comments create
separate `.thread` directories, every reply creates a separate `.message`
directory, and every state or approval transition is a new event file. Review
actions never update a shared comments array.

See [SPEC.md](SPEC.md) for the format contract and [CONTRIBUTING.md](CONTRIBUTING.md)
for development commands.

## Design boundaries

Review Saga intentionally does not replace Git hosting, enforce review policy,
or decide whether an explanation is good. Its core job is narrower: provide a
durable review narrative and make omissions mechanically visible. The local
server binds to loopback by default and has no authentication; do not expose it
to an untrusted network. Interactive fragments execute with scripts enabled but
network access and access to the parent review application are denied.

## License

MIT. See [LICENSE](LICENSE).
