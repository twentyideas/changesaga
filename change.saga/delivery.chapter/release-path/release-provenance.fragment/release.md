# Release provenance {#release-provenance}

The release workflow accepts a pushed `v*` tag or a manual rehearsal, but only a tag-push run may publish. It resolves the tag to a commit, validates SemVer, checks that commit against the event and default branch, and sends the same exact commit through verification and every build job.[^tag-binding]

The archive writer fixes entry order, timestamps, and file modes for both tar.gz and zip output; build staging and same-filesystem replacement keep an earlier artifact intact until its replacement is complete.[^canonical-archive]

Before publication, the workflow rechecks six uploaded archives, creates `SHA256SUMS`, smoke-tests embedded version metadata, attaches build provenance, then refetches the tag and refuses publication if it moved.[^publish-recheck]

[^tag-binding]: Release metadata binds a validated version tag to one default-branch commit before verification or artifact construction.
[^canonical-archive]: Release archives use a canonical three-file shape and deterministic metadata, while staging prevents partial replacement.
[^publish-recheck]: Publication revalidates checksums and tag identity immediately before attestation and GitHub Release creation.
