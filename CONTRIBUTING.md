# Contributing

Thanks for looking. Change Saga is a small format specification plus a reference
Go implementation, and contributions of all sizes are welcome — especially
reproductions, renderer polish, and anything that makes a very large change
easier for a human to finish reviewing.

By contributing you agree that your work is licensed under the repository's
[MIT license](LICENSE). There is no CLA. Participation is covered by the
[Code of Conduct](CODE_OF_CONDUCT.md).

## Before you start

Two commitments shape what gets accepted:

1. **Change Saga authors the material to be reviewed.** It does not conduct
   review. Automated findings, quality scores, and machine approvals are out of
   scope. Coverage is an omission check, never a verdict.
2. **The core stays deterministic, dependency-light, and local.** No hosted
   service, no accounts, no telemetry, no network calls from the CLI.

For anything larger than a bug fix — and for every format change — open an issue
first. It is much cheaper to agree on the shape before the code exists. See
[GOVERNANCE.md](GOVERNANCE.md) for how decisions get made.

## Development setup

Go 1.26 or newer, and Git. Nothing else for the Go side.

```sh
git clone https://github.com/change-saga/change-saga
cd change-saga
go build ./cmd/change-saga
go run ./cmd/change-saga help
```

`.go-version` pins the exact CI and release toolchain patch; update it
deliberately when changing build inputs. `GOTOOLCHAIN=local` is set in CI so a
mismatched toolchain fails loudly instead of silently downloading one. Setting
it locally is a good idea too.

## Before you push

Everything CI checks, in the order it is cheapest to run:

```sh
gofmt -l cmd internal        # must print nothing
go vet ./...
go test -race ./...
go build ./cmd/change-saga
```

If you touched anything under `scripts/` or `.github/workflows/`:

```sh
shellcheck scripts/*.sh
./scripts/check-workflows.sh
./scripts/install_test.sh
./scripts/build-release.sh 0.0.0-dev "$(go env GOOS)" "$(go env GOARCH)" dist
```

If you touched the renderer (`internal/server/`), run the real-browser suite:

```sh
cd e2e
npm ci
npx playwright install chromium
npm test
```

See [e2e/README.md](e2e/README.md) for the browser matrix and what the fixtures
capture on failure.

Docs-only changes still deserve one mechanical pass:

```sh
./scripts/check-docs-links.sh
```

## What CI runs

| Workflow | On | What |
| --- | --- | --- |
| [`ci.yml`](.github/workflows/ci.yml) | every push to `main` and every pull request, forks included | `go test` on Linux, macOS, and Windows (with `-race` where a C toolchain exists), `gofmt`, `go vet`, `shellcheck`, workflow lint/action-pin policy, both installer end-to-end tests, and an unsigned build of all six release targets |
| [`e2e.yml`](.github/workflows/e2e.yml) | every push to `main` and every pull request | Playwright against the real binary and the real server, Chromium by default |
| [`release.yml`](.github/workflows/release.yml) | `v*` tags and manual dispatch only | the full CI matrix again, checksummed artifacts, and optional Developer ID signing/notarization for macOS; only tag-push runs receive provenance/publish permissions and create the GitHub Release |

`ci.yml` is read-only and uses no secrets, so it runs unchanged on pull requests
from forks. All signing lives in `release.yml` behind the protected
`release-signing` environment; manual dispatch is rehearsal-only and cannot
publish.

## Changing the on-disk format

A format change is a single, self-contained piece of history containing:

- the [SPEC.md](SPEC.md) update,
- the [`schema/`](schema) update,
- the implementation and its tests,
- a compatibility note in the pull request: what an older reader does with a
  newer saga, what a newer reader does with an older one, and the migration step
  if there is one,
- a **Format**-marked entry in [CHANGELOG.md](CHANGELOG.md).

`change-saga spec` prints the contract vocabulary that agents rely on; keep it in
step with the spec. See
[docs/releasing.md](docs/releasing.md#versioning-policy) for which kind of change
forces which version bump.

## Conventions

- **Keep authored output deterministic where identity does not require an event.**
  Use sorted iteration and stable identifiers; wall-clock time belongs only in
  append-only review events that explicitly record when something happened.
- **Every reviewer action becomes a small file suitable for Git.** New review
  data means a new file, never a mutation of a shared array — that is what makes
  concurrent review merge cleanly. Withdrawals append; they do not delete.
- **Renderer work follows [docs/renderer-ui.md](docs/renderer-ui.md)**, including
  its vocabulary boundary: reviewer chrome speaks the reviewer's language, while
  format vocabulary belongs to the CLI, the validator, and the spec.
- **Tests come with the change.** A bug fix should come with the test that would
  have caught it.
- **Commit messages** explain why in the body. One logical change per commit;
  keep unrelated reformatting out.

## Pull requests

Open against `main`, fill in the template, and describe the behavior change in
terms a reviewer can check. Draft PRs are welcome for early feedback. A
maintainer's approval merges it; see [GOVERNANCE.md](GOVERNANCE.md).

Since this repository is the reference implementation of Change Saga, a large
pull request is a good excuse to author a saga for it. That is not a
requirement — a plain, clear PR description is always fine.

## Reporting things

- **Bugs and questions** — GitHub issues; see [SUPPORT.md](SUPPORT.md).
- **Security vulnerabilities** — privately, never in an issue; see
  [SECURITY.md](SECURITY.md).
