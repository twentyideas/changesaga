# Change Saga

[![CI](https://github.com/twentyideas/changesaga/actions/workflows/ci.yml/badge.svg)](https://github.com/twentyideas/changesaga/actions/workflows/ci.yml)
[![Browser E2E](https://github.com/twentyideas/changesaga/actions/workflows/e2e.yml/badge.svg)](https://github.com/twentyideas/changesaga/actions/workflows/e2e.yml)
![status: experimental](https://img.shields.io/badge/status-experimental-orange)
![license: MIT](https://img.shields.io/badge/license-MIT-blue)
[![Made with ❤️ using DevSwarm](https://img.shields.io/badge/Made%20with%20%E2%9D%A4%EF%B8%8F%20using-DevSwarm-5F2AFF?labelColor=0A022E&logo=data%3Aimage%2Fpng%3Bbase64%2CiVBORw0KGgoAAAANSUhEUgAAAEAAAAAiCAQAAABFXBcEAAACEElEQVR42s1Y63mDMAw8ugErsAIdwR2BjsAKrMAKWSEdgRXICGQEMsL1RwhIIIFpXhX%2F7E%2F2WY%2BTBKCEAFhxko7TemDPGOmZXzUAQumUt3VHCIKpUimGVRA8MlaONx2ApYKWcg0CAfAgFJrhEAAsuEeKQQsEG7F%2BWLEBQTBXx4Tx%2FSnbXQDa61sJzM%2FMXRss0QpDVtwrlXCeYdWbJNP1CVjgOO5c8JmcCSABM7RIx50TTo4RMwRHv0E27nwnP5wuVuHXyRfQfgFZiB39aUfVYkdn1jIUCYC19iFsH%2BoAUx%2FAoP09njGDNgvF4Zo%2BIopJsnQtAOhklVlUKhvkAoIXKPTSz6UTAmC2tBbdAJcsp5iM0%2Fu7eACDRq3OmgDoO8JwCl2ycNNvC4AO5lqcZlh5aeae2Yg5M9l%2FldFXz9M0Xw4A5uknENvsvwFgYdGjY9GO6VVhVv1oxQXja5qhG2DHVMVF1AYRte1fASxqZ%2BMyRaaviWP%2Frap%2BS8fOqQwK2gcuQjPFK0TfuOKC5mEuaNdc8O4gxDPSMI1NQw4KRt%2F2FCLKjIK3QcX13VRcbVCx2XLLUOz3AYATU5wX%2FICpGr65HI8NSbe85IENSWGPLv%2BjJdtsSuvoprSziH2tKfXb8jO%2BHtaW52iEvtWWv28wecVoFiJHs7cPp%2BZ4Xt49nhc7xnNYE703uqz9oAjiB4VBy1J%2BAQwDuoYAr7YrAAAAAElFTkSuQmCC)](https://devswarm.ai)

Change Saga is experimental. The v2 format may change before 1.0.

Change Saga turns a large code change into a reviewable document. It gives the
change an overview, chapters, diagrams, examples, and links to the exact code
behind each explanation. You can quickly move from any part of the document to
the code diffs it explains, or from a diff back to every relevant part of the
document. Critically, Change Saga validates that every change in the diff is
mapped somewhere in the document. Mapping catches omissions; it is not a claim
that the explanation or code is correct. The result lives in Git beside the
code, can be reviewed a chapter at a time, and takes code review to its next
logical layer above the code itself.

![Change Saga overview](docs/assets/change-saga-overview.gif)

## Install

macOS and Linux:

```sh
curl -fsSL https://raw.githubusercontent.com/twentyideas/changesaga/main/scripts/install.sh | sh
```

Windows PowerShell:

```powershell
irm https://raw.githubusercontent.com/twentyideas/changesaga/main/scripts/install.ps1 | iex
```

Then check the installation:

```sh
change-saga version
change-saga help
```

## Try the example Saga

This repository's own since-inception Saga is attached to the `v0.0.6`
release. After installing Change Saga, download it and open the local reviewer.

macOS and Linux:

```sh
demo_dir="$(mktemp -d)" && \
  curl -fsSL https://github.com/twentyideas/changesaga/releases/download/v0.0.6/change.saga.zip \
    -o "$demo_dir/change.saga.zip" && \
  unzip -q "$demo_dir/change.saga.zip" -d "$demo_dir" && \
  change-saga open "$demo_dir/change.saga"
```

Windows PowerShell:

```powershell
$demo = Join-Path ([IO.Path]::GetTempPath()) ("change-saga-" + [guid]::NewGuid())
New-Item -ItemType Directory -Path $demo | Out-Null
Invoke-WebRequest https://github.com/twentyideas/changesaga/releases/download/v0.0.6/change.saga.zip -OutFile "$demo/change.saga.zip"
Expand-Archive "$demo/change.saga.zip" -DestinationPath $demo
change-saga open "$demo/change.saga"
```

## Quick start

Change Saga is designed to work with the coding agent you already use. Give it
these prompts from the repository containing your change:

**To install the Change Saga skill:**

> Use the change-saga cli to install its skill for this coding agent

**To author a PR's saga:**

> Use the change-saga cli to create a Saga for this PR

**To review a PR's saga:**

> Use the change-saga cli to open this PR's Saga

## What it does

A normal PR description sits above a flat file-by-file diff. That works for
small changes. With a large change, the reviewer has to understand the whole
system while reading isolated files in an arbitrary order.

A saga introduces the change gradually:

1. The overview explains the goal and the shape of the change.
2. Chapters divide it into reviewable pieces—the PRs you might have created if
   you had split the work.
3. Diagrams, interactive HTML, screenshots, and examples show the important
   flows and data models.
4. Each part links to the files and exact diff ranges that implement it.
5. `change-saga status` reports any changed code that has not been accounted
   for.

The tool does not review the code or generate a verdict. It helps the author
prepare the material that other people will review. AI is useful here because
it can build the first draft, create diagrams and examples, and iterate until
the complete diff is represented. The reviewer still decides whether the
change is correct.

Saga content can be Markdown, text, images, SVG, or interactive HTML with
JavaScript. Concrete implementation statements in prose should cite exact diffs
with footnote-style references. Code-bearing diagram nodes and edges should each
link directly to their own files and diff ranges; SVG element bounds become
hoverable links automatically. Headings, exact text, controls, and image regions
can use the same focused mapping model. Everything remains ordinary files in a
`.saga` directory; [SPEC.md](SPEC.md) defines the format.

## Reviewing a saga

`change-saga open` starts a local review application with three views:

- **Saga** presents the overview and chapters. Linked code opens in a large
  drawer without losing the narrative.
- **Code Diff** provides a traditional changed-file tree and diff view, with
  links back to every relevant explanation.
- **Coverage** shows the mapping in both directions: code to explanations and
  explanations to code.

Reviews can be spread across multiple sessions. Reviewers can comment on text
or code, highlight content, draw shapes, add sticky notes, mark files reviewed,
and approve or reject individual parts of the saga.

Every newly initialized saga also carries a small root `README.md`. It tells a
human or AI assistant how to install and open the intended reviewer, and tells
assistants to ask before downloading or executing anything from PR content.

Review data is stored inside the saga. Each comment, reply, annotation, and
state transition gets its own file, which keeps concurrent Git changes small
and avoids shared comment arrays. Attribution comes from the commit that adds
the record.

## Maintain a codebase Saga

A Saga can document a repository over its entire lifetime, not only one PR.
Its authoring and maintenance workflow is designed for AI, not manual human
operation. Maintaining exact coverage, granular citations, diagrams, and
structured evidence by hand would be unreasonable. That exhaustive bookkeeping
is precisely the kind of tedious work AI is good at; humans can focus on
understanding and reviewing the result.

**To create a Saga for the whole codebase:**

> Use the change-saga cli to create a Saga for this codebase since inception

**To update the codebase Saga for a PR:**

> Use the change-saga cli to update this codebase's Saga for the changes in this PR

**To update it from an existing PR Saga:**

> Use the change-saga cli to compare this PR's Saga with the codebase Saga and update what changed

## Structured access for AI

Agents do not need to crawl the saga's files. The CLI exposes a bounded,
read-only JSON interface for the overview, hierarchy, content, reviews,
coverage gaps, diff ownership, mapping quality, author claims, and verification:

```sh
change-saga query overview --saga checkout.saga
change-saga query gaps --saga checkout.saga --kind uncovered
change-saga query mappings --saga checkout.saga --sort scrutiny
change-saga query claims --saga checkout.saga --status unverified
```

`mappings` ranks broad or thin evidence so an AI can start with the weakest
justification. Claims are falsifiable assertions tied to exact code;
verification results are append-only and attributed through Git. An AI review
can inspect the diff cold first, then reconcile its findings against this
structured author account.

Authoring is batchable in the same spirit. `change-saga cover --batch -` reads
newline-delimited JSON records from standard input, resolves the whole batch
before writing anything, and leaves the saga untouched if any record fails:

```sh
printf '%s\n' \
  '{"target":"api.chapter","path":"api.go","side":"new","lines":"18-24","note":"validates the request"}' \
  '{"target":"api.chapter/flow.fragment#submit-action","path":"ui.ts","side":"new","lines":"9","note":"wires the control"}' \
  | change-saga cover --batch - checkout.saga
```

See [the AI-facing interface](docs/ai-facing-interface.md) for the complete
contract.

For a focused file whose entire change belongs to one explanation,
`change-saga cover --path FILE --changed-lines` derives its exact changed-line
and file-event selectors. Coverage summaries can be bounded with `--json` or
silenced with `--quiet`. Repair broad mappings using the `evidence_file` from
`query mappings`: `replace-coverage --record PATH --batch -` atomically splits
or retargets one, while `remove-coverage --record PATH` deletes one.

## Manual CLI workflow

Most people should let their coding agent manage these commands. If you want to
author a saga directly, the basic workflow is:

```sh
change-saga init --base main --head HEAD --title "Checkout rewrite" checkout.saga
change-saga add-chapter --title "Backend" checkout.saga backend
change-saga add-fragment --section backend.chapter --type markdown \
  --id request-flow --title "Request flow" checkout.saga
change-saga set-fragment-content --target request-flow --source ./request-flow.md \
  checkout.saga
change-saga add-landmark --target backend.chapter/request-flow.fragment \
  --heading-id request-validation --label "Request validation" checkout.saga
```

Check for unexplained changes, then open the review UI:

```sh
change-saga status checkout.saga
change-saga open checkout.saga
```

To keep it running in the background and manage it later:

```sh
change-saga open --detach checkout.saga
change-saga serve status checkout.saga
change-saga serve stop checkout.saga
```

Run these commands from the changed repository on the branch containing the
work. If the saga lives in a separate repository, pass
`--repo /path/to/source-checkout` to commands that inspect the diff.

### Maintaining a codebase Saga

Project a PR's source diff onto an existing codebase Saga:

```sh
change-saga compare --repo /path/to/checkout \
  --base <pr-base> --head <pr-head> codebase.saga
```

If the PR already has a Saga, use its source comparison directly:

```sh
change-saga compare --repo /path/to/checkout \
  --against-saga pr-123.saga codebase.saga
```

`compare` never compares prose, diagrams, or other Saga content. It follows
conflicting and nearby source changes through existing evidence ownership and
reports targets that must be updated, targets that should be considered, and
changes that need new content. The command is read-only; use `--json` for CI or
an agent-driven maintenance loop.

## Security

The review application runs only on loopback and refuses remote bind
addresses. Mutations require a per-process token and same-origin requests.
Interactive fragments run in sandboxed frames with network access and parent
application access disabled.

A saga can still contain untrusted HTML, SVG, and JavaScript. Treat one from an
untrusted author with the same care as code from an untrusted branch. See
[SECURITY.md](SECURITY.md) for the threat model and private reporting process.

## Building from source

Change Saga requires Go 1.26.1:

```sh
git clone https://github.com/twentyideas/changesaga
cd change-saga
go build -o ./bin/change-saga ./cmd/change-saga
./bin/change-saga help
```

The release build is a single executable with no separately installed Go
runtime and no hosted service.

## Project links

- [Format specification](SPEC.md)
- [Documentation index](docs/README.md)
- [Contributing](CONTRIBUTING.md)
- [Security policy](SECURITY.md)
- [Support](SUPPORT.md)
- [Governance](GOVERNANCE.md)
- [Changelog](CHANGELOG.md)
- [Release process](docs/releasing.md)

## License

MIT. See [LICENSE](LICENSE).
