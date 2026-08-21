# Security

## Reporting a vulnerability

Please report security issues **privately**. Do not open a public issue, pull
request, or discussion for a suspected vulnerability.

Use GitHub's private vulnerability reporting on this repository:
**Security → Report a vulnerability**. It is visible only to maintainers and
gives us a private thread and, if needed, a private fix branch.

<!--
Repository owners: private vulnerability reporting must be enabled once
(Settings → Code security → Private vulnerability reporting). If you would also
like an email channel, add the address here — it is deliberately absent rather
than invented.
-->

Please include:

- what you observed and what you expected;
- the `change-saga version` output and your OS;
- the smallest reproduction you can manage — a saga fixture, a command sequence,
  or a patch against the test suite is ideal.

We aim to acknowledge a report within a few days and to keep you updated while
we work. This is a volunteer, pre-1.0 project: we will be honest about timelines
rather than promise a fixed SLA. If you plan to disclose publicly, tell us your
intended date and we will work to it.

## Supported versions

Change Saga is pre-1.0 and experimental, and no tagged release exists yet.
Security fixes land on `main`; once releases begin, they will go out in the next
release and older tags will not be patched.

## Threat model

Change Saga is a local, single-user developer tool. It has no hosted or remote
service, no accounts, and no telemetry. Its reviewer is an ephemeral HTTP server
that is deliberately restricted to the loopback interface.

**What the tool defends against**

| Boundary | Behavior |
| --- | --- |
| Network exposure | `serve` and `open` **refuse** any non-loopback listen address. There is no flag that makes the reviewer remotely reachable. |
| DNS rebinding / wrong-host requests | Every request's `Host` header must match the actual listener; others get `403`. |
| Cross-origin requests | Cross-origin protection is enabled, and every mutating request also carries a per-process random token. A hostile page in another tab cannot make the reviewer write to your repository. |
| Interactive fragments | Rendered in `sandbox="allow-scripts"` frames under a restrictive Content-Security-Policy: no network (`connect-src 'none'`), no navigation of the parent, no access to the review application. |
| Path traversal | Fragment assets are served only from inside the saga root. |
| Published release artifacts | Every archive is listed in `SHA256SUMS` and covered by a GitHub build-provenance attestation; installers verify checksums before installing. |

**What it does not defend against**

- **Anyone with access to your loopback port.** There is no authentication.
  Another local user or a process on your machine that can reach the port can
  act as you. That is the reason remote binding is refused outright.
- **Malicious saga content.** A saga is untrusted input: fragments can contain
  arbitrary HTML, SVG, and JavaScript. Sandboxing limits the blast radius, but
  treat opening someone else's saga with the same care as checking out and
  running their branch.
- **Your Git identity.** Review attribution comes from the Git committer of the
  commit that introduced a record. It is an audit trail, not an authentication
  system, and it is exactly as trustworthy as your repository's commit signing
  policy.
- **Anything the CLI is pointed at.** `--repo` and `--source` read from paths you
  supply; the tool does not sandbox your own filesystem from you.

## Reporting non-security bugs

Functional bugs, crashes without a security impact, and format questions belong
in public issues. See [SUPPORT.md](SUPPORT.md).
