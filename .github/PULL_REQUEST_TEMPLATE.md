<!--
Thanks for contributing. Please read CONTRIBUTING.md if you have not already.
Delete any section that does not apply — an empty template helps nobody.
-->

## What this changes

<!-- The behavior change, in terms a reviewer can check. Link the issue it
closes: "Closes #123". -->

## Why

<!-- What was hard or wrong before. -->

## How to verify

<!-- The commands a reviewer should run, and what they should see. -->

```sh

```

## Checks

- [ ] `gofmt -l cmd internal` prints nothing
- [ ] `go vet ./...` and `go test -race ./...` pass
- [ ] Tests cover the change (a bug fix includes the test that would have caught it)
- [ ] Touched `scripts/` or `.github/workflows/`? `shellcheck scripts/*.sh`, `./scripts/check-workflows.sh`, and `./scripts/install_test.sh` pass
- [ ] Touched `internal/server/`? The `e2e/` suite passes and [docs/renderer-ui.md](https://github.com/twentyideas/changesaga/blob/main/docs/renderer-ui.md) still holds
- [ ] Touched docs? `./scripts/check-docs-links.sh` passes
- [ ] User-visible change? [CHANGELOG.md](https://github.com/twentyideas/changesaga/blob/main/CHANGELOG.md) `[Unreleased]` updated

## Format change

<!-- Delete this whole section if the on-disk format is untouched. Otherwise all
of the following belong in this pull request. -->

- [ ] [SPEC.md](https://github.com/twentyideas/changesaga/blob/main/SPEC.md) updated
- [ ] [`schema/`](https://github.com/twentyideas/changesaga/blob/main/schema) updated
- [ ] `change-saga spec` output still matches the spec
- [ ] CHANGELOG entry marked **Format**

**Compatibility:** what an older reader does with a newer saga, what a newer
reader does with an older one, and the migration step if there is one.

<!-- This repository is the reference implementation of Change Saga. For a large
change, authoring a saga for it is welcome but never required — a clear PR
description is always fine. -->
