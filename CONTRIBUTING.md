# Contributing

Change Saga is a small standard plus a reference Go implementation. Changes to
the on-disk format should include a specification update, schema update, and
compatibility discussion in the pull request.

```sh
gofmt -w cmd internal
go test ./...
go vet ./...
go build ./cmd/change-saga
```

Keep the core deterministic and dependency-light. UI work should preserve the
property that every reviewer action becomes a small file suitable for Git, and
should follow the conventions in [docs/renderer-ui.md](docs/renderer-ui.md).

## Release tooling

Pull requests run `.github/workflows/ci.yml`: tests on Linux, macOS, and
Windows, `gofmt`, `shellcheck`, the installer's end-to-end test, and an unsigned
build of all six release targets. It is read-only and uses no secrets, so it
runs unchanged on pull requests from forks.

Anything under `scripts/` or `.github/workflows/` should stay green under:

```sh
shellcheck scripts/*.sh
actionlint
./scripts/install_test.sh
./scripts/build-release.sh 0.0.0-dev "$(go env GOOS)" "$(go env GOARCH)" dist
```

Tagging and macOS signing are documented in [docs/releasing.md](docs/releasing.md).
