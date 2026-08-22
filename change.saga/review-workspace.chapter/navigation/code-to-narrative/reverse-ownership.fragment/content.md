# From a changed line back to its story {#reverse-ownership}

The Code Diff view accepts a qualified file and optional diff URI, rejects selections that do not belong to that file, and preserves the selection in deep links.[^qualified-selection] For the selected atoms, the reverse ownership projection groups matching explanations by chapter and fragment, retaining a landmark anchor when the assignment is narrower than the fragment.[^reverse-link]

The Coverage view is a second projection of the same assignments: one side groups ownership under the repository tree, while the other groups exact chunks under narrative targets.[^two-way-manifest] This makes “code → story” and “story → code” inspectable without inventing a second ownership source.

[^qualified-selection]: Code selection validates the requested file and diff reference against the current comparison before constructing the focused view.
[^reverse-link]: Reverse ownership groups selected atoms by narrative location and links landmark assignments to their exact authored anchor.
[^two-way-manifest]: The coverage manifest derives repository-first and target-first trees from one evaluated ownership report.
