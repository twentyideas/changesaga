# Documentation

Start with the [README](../README.md) for what Change Saga is and how to author
your first saga. This directory holds the deeper material.

## The format

| | |
| --- | --- |
| [SPEC.md](../SPEC.md) | The v2 format contract: model, root and source identity, chapters, sections, fragments, diff URIs, and review overlays. Normative. |
| [`schema/`](../schema) | JSON Schemas for every persisted document, v1 and v2. |
| `change-saga spec` | The same contract as the CLI reports it, with `--json` for agents. If it disagrees with SPEC.md, the CLI wins — please open an issue. |

## Building and shipping

| | |
| --- | --- |
| [releasing.md](releasing.md) | How a release is built, signed, notarized, checksummed, and published, plus the [versioning policy](releasing.md#versioning-policy) and release checklist. |
| [../CHANGELOG.md](../CHANGELOG.md) | Curated release history. |
| [../e2e/README.md](../e2e/README.md) | The real-browser end-to-end suite: how to run it and what it captures on failure. |

## Design notes

These record why the tool looks the way it does. They are history and intent,
not a contract — where they disagree with the code, the code is current.

| | |
| --- | --- |
| [renderer-ui.md](renderer-ui.md) | Conventions the reviewer UI holds to, including the vocabulary boundary between reviewer chrome and format terms. Read before touching `internal/server/`. |
| [ux-reframe.md](ux-reframe.md) | Why review is structured as a sequence of finishable chapter sessions. |
| [review-experience-audit.md](review-experience-audit.md) | The audit behind that reframe, with the session and resumption contract. |
| [ai-facing-interface.md](ai-facing-interface.md) | The application boundary an AI client uses instead of walking `*.chapter` and `___review` paths directly. |
| [ai-query-security-test-plan.md](ai-query-security-test-plan.md) | Adversarial acceptance cases and reusable fixture contract for the AI query boundary. |

## Working on the project

| | |
| --- | --- |
| [../CONTRIBUTING.md](../CONTRIBUTING.md) | Setup, the checks CI runs, and the rules for format changes. |
| [performance.md](performance.md) | Deterministic mega-saga fixtures, benchmark commands, budgets, reference results, and remaining bottlenecks. |
| [../GOVERNANCE.md](../GOVERNANCE.md) | Roles, how decisions are made, and what is out of scope. |
| [../SECURITY.md](../SECURITY.md) | Threat model and private reporting. |
| [../SUPPORT.md](../SUPPORT.md) | Where to ask, and answers to the questions that come up most. |
| [../skills/change-saga/SKILL.md](../skills/change-saga/SKILL.md) | The reference authoring skill behind `change-saga install-skill`. |
