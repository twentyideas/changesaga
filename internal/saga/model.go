package saga

import "time"

const (
	CurrentVersion = 2
	SchemaURL      = "https://reviewsaga.dev/schema/v2/saga.schema.json"
)

type Manifest struct {
	Schema  string `json:"$schema,omitempty"`
	Version int    `json:"version"`
	ID      string `json:"id"`
	Title   string `json:"title"`
	PR      *PR    `json:"pr,omitempty"`
	Source  Source `json:"source"`
}

type PR struct {
	Number int    `json:"number,omitempty"`
	URL    string `json:"url,omitempty"`
}

type Source struct {
	Repository string `json:"repository"`
	Base       string `json:"base"`
	Head       string `json:"head"`
}

type SectionManifest struct {
	Version int    `json:"version"`
	ID      string `json:"id"`
	Title   string `json:"title"`
	Order   int    `json:"order,omitempty"`
}

type ChapterManifest struct {
	Version int    `json:"version"`
	ID      string `json:"id"`
	Title   string `json:"title"`
	Order   int    `json:"order,omitempty"`
}

type Section struct {
	Path      string      `json:"path"`
	Kind      string      `json:"kind"`
	ID        string      `json:"id"`
	Title     string      `json:"title"`
	Order     int         `json:"order,omitempty"`
	Target    string      `json:"target"`
	Children  []*Section  `json:"children,omitempty"`
	Fragments []*Fragment `json:"fragments,omitempty"`
	Diffs     []DiffFile  `json:"diffs,omitempty"`
	Reviews   []Review    `json:"reviews,omitempty"`
}

type FragmentManifest struct {
	Version    int    `json:"version"`
	ID         string `json:"id"`
	Title      string `json:"title,omitempty"`
	MediaType  string `json:"media_type"`
	Entrypoint string `json:"entrypoint"`
	Order      int    `json:"order,omitempty"`
}

type Fragment struct {
	Path       string     `json:"path"`
	Directory  string     `json:"-"`
	ID         string     `json:"id"`
	Title      string     `json:"title,omitempty"`
	MediaType  string     `json:"media_type"`
	Entrypoint string     `json:"entrypoint"`
	Order      int        `json:"order,omitempty"`
	Target     string     `json:"target"`
	Diffs      []DiffFile `json:"diffs,omitempty"`
	Landmarks  []Landmark `json:"landmarks,omitempty"`
	Reviews    []Review   `json:"reviews,omitempty"`
}

type Landmark struct {
	Path      string           `json:"-"`
	Directory string           `json:"-"`
	Version   int              `json:"version"`
	ID        string           `json:"id"`
	Label     string           `json:"label"`
	Selector  LandmarkSelector `json:"selector"`
	Hotspot   *LandmarkRegion  `json:"hotspot,omitempty"`
	Target    string           `json:"target"`
	Diffs     []DiffFile       `json:"diffs,omitempty"`
}

type LandmarkSelector struct {
	Type      string  `json:"type"`
	ElementID string  `json:"element_id,omitempty"`
	HeadingID string  `json:"heading_id,omitempty"`
	Exact     string  `json:"exact,omitempty"`
	Prefix    string  `json:"prefix,omitempty"`
	Suffix    string  `json:"suffix,omitempty"`
	X         float64 `json:"x,omitempty"`
	Y         float64 `json:"y,omitempty"`
	Width     float64 `json:"width,omitempty"`
	Height    float64 `json:"height,omitempty"`
}

type LandmarkRegion struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

type DiffFile struct {
	Path    string          `json:"-"`
	Version int             `json:"version"`
	Diffs   []DiffReference `json:"diffs"`
}

type DiffReference struct {
	URI  string `json:"uri"`
	Note string `json:"note,omitempty"`
}

type Review struct {
	Path              string    `json:"-"`
	AttributionDetail string    `json:"-"`
	Version           int       `json:"version"`
	ID                string    `json:"id"`
	Author            string    `json:"author,omitempty"`
	State             string    `json:"state"`
	Body              string    `json:"body,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
}

type Thread struct {
	Version           int           `json:"version"`
	ID                string        `json:"id"`
	Target            string        `json:"target"`
	Anchor            Anchor        `json:"anchor"`
	Kind              string        `json:"kind,omitempty"`
	Suggestion        *Suggestion   `json:"suggestion,omitempty"`
	CreatedBy         string        `json:"created_by,omitempty"`
	CreatedAt         time.Time     `json:"created_at"`
	Directory         string        `json:"-"`
	AttributionDetail string        `json:"-"`
	Messages          []*Message    `json:"messages,omitempty"`
	Events            []ThreadEvent `json:"events,omitempty"`
	State             string        `json:"state"`
}

type ThreadManifest struct {
	Version    int         `json:"version"`
	ID         string      `json:"id"`
	Target     string      `json:"target"`
	Anchor     Anchor      `json:"anchor"`
	Kind       string      `json:"kind,omitempty"`
	Suggestion *Suggestion `json:"suggestion,omitempty"`
	CreatedBy  string      `json:"created_by,omitempty"`
	CreatedAt  time.Time   `json:"created_at"`
}

type Anchor struct {
	Type       string        `json:"type"`
	Shapes     []Shape       `json:"shapes,omitempty"`
	Text       *TextSelector `json:"text,omitempty"`
	Note       *NoteSelector `json:"note,omitempty"`
	Diff       *DiffSelector `json:"diff,omitempty"`
	Coordinate string        `json:"coordinate_space,omitempty"`
}

type DiffSelector struct {
	URI string `json:"uri"`
}

type Suggestion struct {
	Replacement string `json:"replacement"`
}

type Shape struct {
	Type        string  `json:"type"`
	X           float64 `json:"x,omitempty"`
	Y           float64 `json:"y,omitempty"`
	Width       float64 `json:"width,omitempty"`
	Height      float64 `json:"height,omitempty"`
	Points      []Point `json:"points,omitempty"`
	Color       string  `json:"color,omitempty"`
	StrokeWidth float64 `json:"stroke_width,omitempty"`
}

type Point struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type TextSelector struct {
	Exact  string `json:"exact"`
	Prefix string `json:"prefix,omitempty"`
	Suffix string `json:"suffix,omitempty"`
	Start  int    `json:"start,omitempty"`
	End    int    `json:"end,omitempty"`
	Color  string `json:"color,omitempty"`
}

type NoteSelector struct {
	Text  string  `json:"text"`
	X     float64 `json:"x"`
	Y     float64 `json:"y"`
	Color string  `json:"color,omitempty"`
}

type MessageManifest struct {
	Version   int       `json:"version"`
	ID        string    `json:"id"`
	Author    string    `json:"author,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type Message struct {
	Path              string      `json:"-"`
	AttributionDetail string      `json:"-"`
	ID                string      `json:"id"`
	Author            string      `json:"author,omitempty"`
	CreatedAt         time.Time   `json:"created_at"`
	Fragments         []*Fragment `json:"fragments"`
}

type ThreadEvent struct {
	Path              string    `json:"-"`
	AttributionDetail string    `json:"-"`
	Version           int       `json:"version"`
	ID                string    `json:"id"`
	Author            string    `json:"author,omitempty"`
	State             string    `json:"state,omitempty"`
	Anchor            *Anchor   `json:"anchor,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
}

type DiffReview struct {
	Path              string    `json:"-"`
	AttributionDetail string    `json:"-"`
	Version           int       `json:"version"`
	ID                string    `json:"id"`
	URI               string    `json:"uri"`
	Author            string    `json:"author,omitempty"`
	State             string    `json:"state"`
	CreatedAt         time.Time `json:"created_at"`
}

type Saga struct {
	Root        string       `json:"root"`
	Manifest    Manifest     `json:"manifest"`
	Section     *Section     `json:"section"`
	Threads     []*Thread    `json:"threads,omitempty"`
	DiffReviews []DiffReview `json:"diff_reviews,omitempty"`
}

type Issue struct {
	Severity string `json:"severity"`
	Path     string `json:"path"`
	Message  string `json:"message"`
}

type Validation struct {
	Valid  bool    `json:"valid"`
	Issues []Issue `json:"issues"`
}

func SagaTarget(sagaID string) string {
	return "urn:review-saga:" + sagaID + ":saga"
}

func SectionTarget(sagaID, sectionID string) string {
	return "urn:review-saga:" + sagaID + ":section:" + sectionID
}

func ChapterTarget(sagaID, chapterID string) string {
	return "urn:review-saga:" + sagaID + ":chapter:" + chapterID
}

func FragmentTarget(sagaID, fragmentID string) string {
	return "urn:review-saga:" + sagaID + ":fragment:" + fragmentID
}

func LandmarkTarget(sagaID, fragmentID, landmarkID string) string {
	return FragmentTarget(sagaID, fragmentID) + ":landmark:" + landmarkID
}
