# Slide-native Change Saga

This is a v4 visual review deck, not a paginated report.

Author with `change-saga add-deck`, `change-saga add-slide`, and `change-saga add-item`.
Every meaningful visual node, edge, region, transition, or callout is an Item.
Attach exact diff evidence to Items with `change-saga cover`; deck- and slide-level evidence is refused.

Before authoring, storyboard the reviewer question and truthful visual form of
each slide. Use boundaries for systems, containment/dependencies for
architecture, directed edges for data flow, lanes/messages for sequence,
states and labeled transitions for lifecycle, entities and cardinalities for
data models, branches for logic, and trigger/propagation/containment/recovery
for failure paths. A row of labeled cards is not a default diagram.

Build a surprise inventory before drawing. Establish the minimum system model,
then identify where a reasonable reviewer expectation differs from the actual
behavior: hidden coupling, counterintuitive outcomes, consequential constraints,
tradeoffs, or intentional deviations from repository norms. Show expectation,
actual behavior, rationale, and consequence together and link them to exact
evidence. Surprises are especially good callout Items: attach the callout to the
node, edge, state, or transition that creates the surprise.
Do not manufacture novelty when the change has none.

Audit reviewer surprise as well as the deck's contact sheet before handoff. If
the reviewer cannot name the system model, the consequential deviation, why it
exists, and its tradeoff—or if slides remain
indistinguishable after labels and colors are ignored—or several unrelated
questions use the same primitive topology—rewrite them before mapping more
evidence. Coverage detects omissions; it cannot turn a weak visual into an explanation.

The package is intentionally flat and compact. Treat category-prefixed filenames
as private storage; use stable IDs, target URNs, and `change-saga query`.
