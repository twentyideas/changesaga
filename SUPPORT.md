# Getting help

Change Saga is an experimental, volunteer-maintained project. There is no
commercial support, no hosted service, and no paid tier — just this repository.

## Start here

1. **`change-saga help`** and **`change-saga <command> -h`** are the source of
   truth for commands and flags. They ship with your binary, so they always
   match it.
2. **[README.md](README.md)** — install, the authoring loop, and a first saga.
3. **[SPEC.md](SPEC.md)** and `change-saga spec` — what the on-disk format
   guarantees. If the CLI and the docs disagree, the CLI wins; please report it.
4. **[docs/](docs/README.md)** — releasing, renderer conventions, and the design
   notes behind the current UX.

## Common questions

**`status` exits 3 and I think it's wrong.** Exit 3 means "coverage incomplete",
not "error". Run `change-saga status --json` to see every unaccounted diff atom
by path, side, and line. If an atom you have already covered still shows up,
check that `--side` matches (deleted lines are `old`, added lines are `new`) and
that the `--path` is the repository-relative path Git reports.

**"Stale URIs" appear after I rebase.** Evidence URIs pin the resolved base and
a digest of the product patch. Rewriting the compared history invalidates them
by design. Re-run `cover` for the affected ranges.

**My saga lives in a different repository from the code.** Pass
`--repo /path/to/source-checkout` to `cover`, `status`, and `open`. Local
checkout paths are runtime configuration and are never committed.

**`init` refuses to record a repository identity.** The saga format stores a
portable absolute repository URI. Without a usable `origin`, pass
`--repository <uri>` or opt in explicitly with `--allow-local-repository`.

**The reviewer won't start on the address I gave it.** By design: `serve` and
`open` accept only loopback addresses. See [SECURITY.md](SECURITY.md).

## Asking a question

Open a **GitHub issue** using the *Question* template. Include your
`change-saga version`, your OS, the exact command you ran, and its output.

For bugs, use the *Bug report* template — a reproduction beats a description
every time. For format or workflow proposals, use *Idea or proposal*; changes to
the on-disk format are decided in the open, and it helps to discuss before
writing code.

## What not to use issues for

- **Security vulnerabilities.** Report them privately — see
  [SECURITY.md](SECURITY.md).
- **Code of conduct concerns.** See [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md).

## Response expectations

Maintainers read everything, but this is not anyone's day job. Expect days, not
hours. Issues that include a reproduction are answered soonest, and a pull
request with a failing test attached is the fastest path of all — see
[CONTRIBUTING.md](CONTRIBUTING.md).
