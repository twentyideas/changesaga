//go:build !race

package server

// raceDetectorActive reports whether this test binary was built with -race.
// The race detector changes what an allocation measurement means — it roughly
// doubles the bytes a request allocates — so absolute allocation budgets are
// only meaningful without it. Growth ratios are unaffected, because both sides
// of a comparison pay the same overhead, and remain asserted either way.
const raceDetectorActive = false
