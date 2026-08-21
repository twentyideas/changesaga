# Real-browser end-to-end tests

This suite builds and runs the real `change-saga` binary. Every test creates
fresh, separate source and saga Git repositories, commits a deterministic
source comparison, generates exact coverage through the CLI, and starts the
server on an ephemeral loopback port.

## Run locally

Use the Node version in `.node-version`, then run:

```sh
npm ci
npx playwright install chromium
npm test
```

`npm run test:all-browsers` also runs the defined Firefox and WebKit projects.
`npm run test:repeat-critical` repeats the mutation-heavy critical flows three
times to check isolation and timing.

On failure, Playwright retains the trace, screenshot, and video. The fixture
also attaches browser console/network events, server output, and a sanitized
snapshot of both temporary repositories before deleting them.

## Layers

Each check runs at the cheapest layer that can still tell the truth about it.

- **Browser** (`navigation`, `annotations`, `review`, `accessibility`): a real
  Chromium page against the real server process. Reviewer behavior, rendering,
  focus, and axe scans live here.
- **HTTP against the running process** (`security`): raw Node requests to the
  spawned server, which is the only way to forge the `Host` and `Origin`
  headers a browser refuses to send. Used for the session-token, cross-origin,
  diff-identity, and upload rejections.
- **Subprocess** (`cli`, plus the non-loopback check in `security`): the real
  binary invoked with real arguments, asserting exit status, message, and that
  a refusal wrote nothing.

Byte-boundary edges that would make an end-to-end request unreliable — such as
a request larger than the 32 MiB multipart cap, where the server answers before
the client finishes writing — stay in the Go unit tests
(`internal/server`, `internal/reviewstore`, `internal/store`). This suite
asserts the contract those limits exist to provide.

## Fixtures

- `saga`: both repositories, a running server, and a page already loaded.
- `sagaRepositories`: both repositories only, with no server and no browser, for
  subprocess tests.

Both hand the server subprocess a private `TMPDIR` inside the fixture, so
`stagedUploads()` can prove a rejected upload left no staged file behind.

The fixture source repository has an `origin` matching the declared repository
URI, because the CLI verifies declared identity against the checkout's origin on
every read. `identity` on the fixture carries the repository URI and the base and
head object IDs, and `canonicalFileURI` / `canonicalLineURI` build the exact
canonical spelling the product accepts. A positive control in `security.spec.ts`
asserts that spelling is byte-for-byte what the server itself renders, so the
malformed and non-canonical cases cannot pass vacuously.

## Zero-side-effect assertions

`treeSnapshot()` records every path and size under a directory. A rejection test
snapshots the saga before the rejected requests and compares afterwards, so a
partial write, a stray lock file, or a rewritten record all fail the test.

## Accessibility

`expectNoSeriousAccessibilityViolations(page)` scans the whole page — chrome
included — for serious and critical axe violations.

The code and coverage views still fail `color-contrast` on their muted counts
and metrics. That gap is pinned with `expectOnlyKnownAccessibilityGaps(page,
["color-contrast"])`, which disables no rule and excludes no node: it compares
the complete serious/critical result against an exact rule list, so a new
violation, a new rule, or a fixed rule all fail and force the list to be
revisited.
