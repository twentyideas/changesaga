# Contributing

Review Saga is a small standard plus a reference Go implementation. Changes to
the on-disk format should include a specification update, schema update, and
compatibility discussion in the pull request.

```sh
gofmt -w cmd internal
go test ./...
go vet ./...
go build ./cmd/saga
```

Infrastructure changes live in `infra/` and use their own toolchain:

```sh
cd infra
npm install
npm run check   # format:check, build, test, synth
```

`npm test` and `npm run synth` make no AWS calls and need no credentials.

Keep the core deterministic and dependency-light. UI work should preserve the
property that every reviewer action becomes a small file suitable for Git.
