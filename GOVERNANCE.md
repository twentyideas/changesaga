# Governance

Change Saga is a small, pre-1.0 project: a format specification plus a reference
Go implementation. Governance is deliberately lightweight, and this document
describes what actually happens rather than an aspirational process.

## Roles

**Contributors** are anyone who opens an issue, comments, reviews, or sends a
pull request. No agreement or CLA is required; contributions are accepted under
the repository's [MIT license](LICENSE), as stated in
[CONTRIBUTING.md](CONTRIBUTING.md).

**Maintainers** are the accounts with write access to this repository. They
triage issues, review and merge pull requests, cut releases, and hold the
signing and publishing credentials. The current list is the repository's
*Settings → Collaborators and teams* roster, shown publicly on the repository's
**Contributors** and **People** pages.

<!--
Repository owners: if you prefer an explicit, reviewable list, replace the
paragraph above with a table of maintainers (name, GitHub handle, area) and add
a .github/CODEOWNERS file so review requests route automatically. It is left
implicit here rather than inventing names.
-->

Maintainers are added by consensus of the existing maintainers, normally after a
sustained history of good review and contribution. Maintainers who are inactive
for a long stretch may be moved to emeritus status; this is administrative, not
a judgement.

## How decisions are made

Decisions are made in public, in issues and pull requests. In practice:

- **Ordinary changes** — bug fixes, tests, docs, renderer work — need one
  maintainer's approval. Lazy consensus applies: if nobody objects, it merges.
- **Anything that changes the on-disk format** needs explicit maintainer
  agreement and, per [CONTRIBUTING.md](CONTRIBUTING.md), must arrive as a single
  change covering [SPEC.md](SPEC.md), the JSON Schemas in [`schema/`](schema),
  the implementation, and a written compatibility note.
- **Disagreements** are resolved by discussion first. If that stalls, the
  maintainers decide by simple majority; a tie means the status quo stands.
- **Security matters** are handled privately until a fix ships. See
  [SECURITY.md](SECURITY.md).

## Scope

Two commitments shape what gets accepted, and it saves everyone time to know
them before proposing large work:

1. **Change Saga authors the material to be reviewed.** Features that conduct
   review — automated findings, quality scoring, machine approvals — are out of
   scope, including AI-generated ones. Coverage is an omission check, never a
   verdict on whether an explanation is good.
2. **The core stays deterministic, dependency-light, and local.** A saga is
   ordinary files in Git. There is no hosted service, no account, and no
   telemetry, and proposals that require one will be declined.

Beyond those, the project is open to a wide range of contributions — especially
renderer work, format ergonomics, and anything that makes very large changes
easier for a human to finish reviewing.

## Releases

Any maintainer may cut a release by pushing a `v<major>.<minor>.<patch>` tag;
everything after that is automated. Versioning and release policy are described
in [docs/releasing.md](docs/releasing.md) and the history is in
[CHANGELOG.md](CHANGELOG.md).

## Changing this document

Amendments go through a pull request like anything else and need agreement from
the maintainers.
