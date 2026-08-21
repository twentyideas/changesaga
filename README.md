# Review Saga

Review Saga is an experimental, Git-native way to author changes that are too
large to explain in one flat pull-request description and diff. A saga is the
change proposal submitted for review: it organizes the work into a big-picture
overview and independently reviewable chapters—roughly the PRs that might have
existed if the change were split. Sections and fragments recurse inside each
chapter, and the tool proves that every changed line appears somewhere in that
story.

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
  replies, fragment attachments, in-memory undo/redo while composing, and
  selectable committed shapes that can be moved, recolored, or removed.
- A Saga view with per-fragment diff drawers, a full Code Diff view, and a
  bidirectional Coverage Manifest proving every code-to-narrative mapping.
- Consistent saga-, section-, and fragment-level approvals.

## Install

macOS and Linux, from the latest release:

```sh
curl -fsSL https://raw.githubusercontent.com/review-saga/review-saga/main/scripts/install.sh | sh
```

Windows PowerShell:

```powershell
irm https://raw.githubusercontent.com/review-saga/review-saga/main/scripts/install.ps1 | iex
```

Both installers detect the operating system architecture, download the matching
asset from the latest GitHub Release, and verify it against the release
`SHA256SUMS`. The shell installer writes to `~/.local/bin` and never calls
`sudo`. The PowerShell installer writes to the current user's local application
directory and adds it to the user `PATH`; it never requests elevation.

Pass Unix options after `-s --`:

```sh
curl -fsSL .../install.sh | sh -s -- --version v0.3.0 --dir ~/bin
```

For a pinned Windows version, download the script before invoking it:

```powershell
irm https://raw.githubusercontent.com/review-saga/review-saga/main/scripts/install.ps1 -OutFile install.ps1
.\install.ps1 -Version v0.3.0
```

Prefer to do it by hand? Download the archive for your platform from the
[releases page](https://github.com/review-saga/review-saga/releases), then
verify it before unpacking:

```sh
sha256sum -c SHA256SUMS --ignore-missing
gh attestation verify review-saga_0.3.0_linux_amd64.tar.gz --repo review-saga/review-saga
```

From source, with Go 1.26 or newer:

```sh
go build -o ./bin/review-saga ./cmd/review-saga
```

After the project is published at its provisional module path, installation can
use `go install github.com/review-saga/review-saga/cmd/review-saga@latest`.

During local development:

```sh
go run ./cmd/review-saga help
```

## A first saga

Create a saga for a branch compared with the merge base of `main` and `HEAD`:

```sh
review-saga init --base main --head HEAD --title "Checkout rewrite" pr-1234.saga
review-saga add-chapter --title "Backend behavior" pr-1234.saga backend
review-saga add-section --title "Request flow" pr-1234.saga backend.chapter/request-flow
review-saga add-fragment --section backend.chapter/request-flow --type html \
  --title "Try the request flow" pr-1234.saga
review-saga status --json pr-1234.saga
```

To import a complete interactive package, pass its directory; `index.html` is
the default HTML entrypoint and can be overridden with `--entrypoint`:

```sh
review-saga add-fragment --section backend.chapter/request-flow --type html \
  --source ./review-demos/request-flow --entrypoint index.html pr-1234.saga
```

The JSON status lists every uncovered diff atom. An author or AI agent can group
those atoms into coherent sections and attach evidence:

```sh
review-saga cover \
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
absolute source repository URI, resolved base identity, and a stable digest of
the product patch. Saga-only commits do not invalidate the digest. For a saga in
a separate repository, add `--repo /path/to/source-checkout` to `cover`,
`status`, and `open`.

Continue until the check succeeds, then open the local review:

```sh
review-saga status pr-1234.saga
review-saga open pr-1234.saga
```

`status` exits with code 3 while coverage is incomplete, which makes it suitable
for an authoring loop or CI. `validate` checks only the on-disk structure and
exits with code 1 for schema errors.

## AI-guided authoring

Print a portable prompt that asks the active coding agent to install the
project-local authoring skill using its own native mechanism:

```sh
review-saga install-skill
```

The command does not write files or assume Codex, Claude Code, OpenCode, or any
other agent layout. Copy its output into the agent working in the target
repository. The installed skill preserves that repository's normal PR-drafting
process while expressing the result as a visual, coverage-complete Review Saga.
It explicitly authors the thing submitted for review; it does not conduct the
review or create review feedback.

This repository also keeps the full reference
[`review-saga` skill](skills/review-saga/SKILL.md) used to develop that portable
contract.

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
            ├── ___landmarks/
            │   └── submit-action.landmark/
            │       ├── landmark.json
            │       └── ___diffs/
            └── ___diffs/
```

Every root `.chapter` directory is an independent review boundary, every
ordinary nested directory is a section, and every `.fragment` directory is an
atomic content package. Review threads live in a separate overlay and their
messages contain fragments too, so comments can be Markdown, images, SVG, or
interactive HTML rather than one text field.

Markdown headings carry explicit `{#stable-anchor}` markers. Addressable
HTML/SVG elements, exact text, and image regions use independent
`___landmarks/<id>.landmark/` packages. Their own `___diffs/` files connect an
exact narrative element to exact code, and static-media hotspots reveal link
and code controls on hover.

The local reviewer has three complementary surfaces. **Saga** keeps the authored
narrative in the foreground and opens attached code in a scrollable right-hand
drawer. The drawer starts with collapsed source files and their authored
what-and-why summaries; expanding a file reveals only the ranges linked to that
narrative target. **Code Diff** presents the entire comparison with a changed-file tree.
**Manifest**
audits the relationship in both directions: every changed range shows the Saga
elements that explain it, and every mapped Saga element shows its exact code
ranges. The review surfaces use the same diff rows and thread records, so a
comment or suggestion made in one is visible in the others. Reviewed-file
markers and fragment approvals are append-only metadata committed with the saga.
Sticky notes are first-class annotations: choose **Sticky** in the annotation
toolbox, click where the note belongs on a fragment, type it, and commit. A
committed note is a placed, hyperlinkable review entity that can be selected,
dragged, reworded, recolored, and removed with `Delete`.

Undo and redo apply to the transient annotation canvas before submission. Once
committed, shapes and notes are edited directly; moving, rewording, or
recoloring appends an anchor event, and removing appends a `withdrawn` state
event. Git therefore retains the original comment and every committed
transition.

Review data is intentionally conflict-resistant: separate comments create
separate `.thread` directories, every reply creates a separate `.message`
directory, and every state or approval transition is a new event file. Review
actions never update a shared comments array.

See [SPEC.md](SPEC.md) for the format contract, [CONTRIBUTING.md](CONTRIBUTING.md)
for development commands, and [docs/releasing.md](docs/releasing.md) for how
releases are built, signed, and verified.

## Hosting a shareable saga

The local reviewer is loopback-only. [`infra/`](infra/README.md) contains an AWS
CDK v2 app that provisions the hosting foundation for *shareable* Review Saga
sites: a private S3 bucket holding the generic renderer shell, CloudFront in
front of it using Origin Access Control, and an API Gateway HTTP API boundary
with a small health/config Lambda routed at `/api/*`.

That boundary is where GitHub App authentication, comment posting, and private
saga delivery will land; none of them exist yet. Saga narratives, review
threads, and code diffs are private per-repository data and are never placed in
the public bucket — synthesis fails if they are. See
[infra/README.md](infra/README.md) for architecture, deployment, cost drivers,
security boundaries, and next steps.

## Design boundaries

Review Saga intentionally does not replace Git hosting, enforce review policy,
or decide whether an explanation is good. Its core job is narrower: let authors
draft a durable, visual change proposal and make omissions mechanically
visible. The local
server binds to loopback by default and has no authentication; do not expose it
to an untrusted network. Interactive fragments execute with scripts enabled but
network access and access to the parent review application are denied.

## License

MIT. See [LICENSE](LICENSE).
