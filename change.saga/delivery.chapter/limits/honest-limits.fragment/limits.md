# Honest limits and external checks {#honest-limits}

- Checksums prove that a downloaded archive matches the separately fetched release manifest; they do not independently prove the publisher's identity. GitHub provenance attestation and macOS signing add distinct signals, and consumers must verify them when that assurance matters.[^checksum-limit]
- Apple notarization depends on external credentials and service acceptance. A bare CLI cannot carry a stapled ticket, so Gatekeeper may require an online lookup; unsigned and signed-but-not-notarized releases are disclosed rather than presented as equivalent.[^notarization-limit]
- The documentation link checker validates repository-relative destinations and anchors but deliberately does not fetch external URLs.[^link-limit]
- The checked-in Playwright suite defines supported browser behavior, but this chapter does not claim a fresh cross-platform browser run unless the verification record says so.[^browser-limit]
- Published M3 Pro performance figures are historical reference measurements. The deterministic workload and budgets are reviewable; those exact numbers are not asserted for this workspace.[^benchmark-limit]

[^checksum-limit]: Installers compare release bytes to SHA256SUMS while the publish job separately emits provenance attestations for stronger origin verification.
[^notarization-limit]: Signing and notarization are credential- and service-dependent, and release notes expose the achieved macOS state without disabling platform protections.
[^link-limit]: Documentation checking is offline and deterministic, so external destination availability remains outside its verified boundary.
[^browser-limit]: Browser coverage is encoded as executable Playwright scenarios and CI jobs, but platform execution remains an explicit verification obligation.
[^benchmark-limit]: Performance results name their hardware and command, and the document labels wall-clock budgets as investigative rather than cross-platform guarantees.
