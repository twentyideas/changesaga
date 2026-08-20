// Not a Go project. This file exists only so that `go build ./...`,
// `go vet ./...`, and `go test ./...` at the repository root skip this
// directory: Go treats a directory containing go.mod as a separate module and
// excludes it from the parent module's `./...` pattern. Without it, `npm
// install` leaves Go source under infra/node_modules that breaks those
// commands.
module github.com/review-saga/review-saga/infra

go 1.26
