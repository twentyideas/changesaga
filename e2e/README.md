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
