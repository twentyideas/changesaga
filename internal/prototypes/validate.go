package prototypes

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/twentyideas/changesaga/internal/livingid"
)

var (
	digestPattern      = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	cssPropertyPattern = regexp.MustCompile(`^--[A-Za-z0-9_-]+$`)
	cssClassPattern    = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]{0,127}$`)
	unsafeCSS          = []*regexp.Regexp{
		regexp.MustCompile(`(?i)@import\b`),
		regexp.MustCompile(`(?i)url\s*\(`),
		regexp.MustCompile(`(?i)expression\s*\(`),
		regexp.MustCompile(`(?i)(?:^|[;{])\s*behavior\s*:`),
		regexp.MustCompile(`(?i)-moz-binding\s*:`),
	}
)

type validationErrors struct{ values []error }

func (v *validationErrors) add(format string, args ...any) {
	v.values = append(v.values, fmt.Errorf(format, args...))
}
func (v *validationErrors) err() error {
	if len(v.values) == 0 {
		return nil
	}
	return errors.Join(v.values...)
}

func validateIdentity(value Identity, expectedID string) error {
	var p validationErrors
	if value.Schema != IdentitySchemaURL {
		p.add("$schema must be %q", IdentitySchemaURL)
	}
	if value.Version != Version {
		p.add("version must be %d", Version)
	}
	if !livingid.ValidID(value.ID) || value.ID != expectedID {
		p.add("prototype id must be stable and match its package name")
	}
	validateTime(&p, value.CreatedAt)
	validateRequestID(&p, value.RequestID)
	return p.err()
}

func validateRevision(value Revision, sagaID, prototypeID string) error {
	var p validationErrors
	if value.Schema != RevisionSchemaURL {
		p.add("$schema must be %q", RevisionSchemaURL)
	}
	if value.Version != Version {
		p.add("version must be %d", Version)
	}
	if !livingid.ValidID(value.ID) {
		p.add("revision id is not a stable identifier")
	}
	want, _ := PrototypeURN(sagaID, prototypeID)
	if value.Prototype != want {
		p.add("prototype must be %q", want)
	}
	if strings.TrimSpace(value.Title) == "" {
		p.add("title is required")
	}
	if value.State != StateDraft && value.State != StateReady && value.State != StateRetired {
		p.add("state must be draft, ready, or retired")
	}
	seen := map[string]bool{}
	for _, parent := range value.Parents {
		if _, err := parseRevisionURN(parent, sagaID, prototypeID); err != nil {
			p.add("parent %q %v", parent, err)
		} else if seen[parent] {
			p.add("parent %q is duplicated", parent)
		}
		seen[parent] = true
	}
	validateSource(&p, value.Source)
	if value.Source.Kind != SourceHTML && len(value.Styles) > 0 {
		p.add("repository styles are allowed only for html prototypes")
	}
	seenStyles := map[string]bool{}
	for i := range value.Styles {
		validateStyle(&p, fmt.Sprintf("styles[%d]", i), value.Styles[i])
		if seenStyles[value.Styles[i].Path] {
			p.add("stylesheet path %q is duplicated", value.Styles[i].Path)
		}
		seenStyles[value.Styles[i].Path] = true
	}
	validateTime(&p, value.CreatedAt)
	validateRequestID(&p, value.RequestID)
	return p.err()
}

func validateSource(p *validationErrors, source Source) {
	switch source.Kind {
	case SourceHTML:
		if !safeRelativePath(source.Entrypoint) || filepath.Ext(source.Entrypoint) != ".html" {
			p.add("html entrypoint must be a safe relative .html path")
		}
		if !digestPattern.MatchString(source.ContentDigest) {
			p.add("html content_digest must use sha256:<64 lowercase hex>")
		}
		if source.URL != "" || source.EmbedURL != "" || source.FallbackURL != "" || source.Allowlist != nil {
			p.add("html source cannot contain external source fields")
		}
	case SourceExternal:
		if !absoluteHTTPURL(source.URL) {
			p.add("external url must be an absolute http or https URL")
		}
		if source.Entrypoint != "" || source.ContentDigest != "" || source.EmbedURL != "" || source.FallbackURL != "" || source.Allowlist != nil {
			p.add("external source contains fields reserved for html or embed")
		}
	case SourceEmbed:
		if !absoluteHTTPURL(source.EmbedURL) {
			p.add("embed_url must be an absolute https URL")
		}
		if parsed, _ := url.Parse(source.EmbedURL); parsed == nil || parsed.Scheme != "https" {
			p.add("embed_url must use https")
		}
		if !absoluteHTTPURL(source.FallbackURL) {
			p.add("fallback_url is required and must be absolute")
		}
		if source.Allowlist == nil {
			p.add("embed source requires explicit allowlist metadata")
		} else {
			validateAllowlist(p, *source.Allowlist, source.EmbedURL)
		}
		if source.Entrypoint != "" || source.ContentDigest != "" || source.URL != "" {
			p.add("embed source contains fields reserved for html or external")
		}
	default:
		p.add("source kind must be html, external, or embed")
	}
}

func validateAllowlist(p *validationErrors, value ProviderAllowlist, embedURL string) {
	if !livingid.ValidID(value.Provider) {
		p.add("allowlist provider must be a stable identifier")
	}
	origin, ok := declaredOrigin(value.EmbedOrigin)
	embed, embedOK := originOfURL(embedURL)
	if !ok || !embedOK || origin != embed {
		p.add("allowlist embed_origin must exactly match embed_url origin")
	}
	allowedSandbox := map[string]bool{"allow-forms": true, "allow-modals": true, "allow-popups": true, "allow-popups-to-escape-sandbox": true, "allow-presentation": true, "allow-same-origin": true, "allow-scripts": true}
	allowedPermissions := map[string]bool{"clipboard-read": true, "clipboard-write": true, "fullscreen": true}
	validateStringSet(p, "allowlist sandbox", value.Sandbox, allowedSandbox)
	validateStringSet(p, "allowlist permissions", value.Permissions, allowedPermissions)
}

func validateStringSet(p *validationErrors, name string, values []string, allowed map[string]bool) {
	seen := map[string]bool{}
	for _, value := range values {
		if !allowed[value] {
			p.add("%s contains unsupported value %q", name, value)
		}
		if seen[value] {
			p.add("%s duplicates %q", name, value)
		}
		seen[value] = true
	}
}

func validateStyle(p *validationErrors, name string, style StyleSource) {
	if !safeRelativePath(style.Path) || filepath.Ext(style.Path) != ".css" {
		p.add("%s path must be a safe relative .css path", name)
	}
	if !digestPattern.MatchString(style.Digest) {
		p.add("%s digest must use sha256:<64 lowercase hex>", name)
	}
	seenProperties := map[string]bool{}
	for _, property := range style.CustomProperties {
		if !cssPropertyPattern.MatchString(property) {
			p.add("%s custom property %q is invalid", name, property)
		}
		if seenProperties[property] {
			p.add("%s custom property %q is duplicated", name, property)
		}
		seenProperties[property] = true
	}
	seenRoles := map[string]bool{}
	for _, role := range style.Roles {
		if !livingid.ValidID(role.Role) || !cssClassPattern.MatchString(role.Class) {
			p.add("%s role/class mapping %q/%q is invalid", name, role.Role, role.Class)
		}
		if seenRoles[role.Role] {
			p.add("%s role %q is duplicated", name, role.Role)
		}
		seenRoles[role.Role] = true
	}
}

func validateAnnotation(value Annotation, sagaID, expectedID string) error {
	var p validationErrors
	if value.Schema != AnnotationSchemaURL {
		p.add("$schema must be %q", AnnotationSchemaURL)
	}
	if value.Version != Version {
		p.add("version must be %d", Version)
	}
	if !livingid.ValidID(value.ID) || value.ID != expectedID {
		p.add("annotation id must be stable and match its filename")
	}
	prototypeID, err := parsePrototypeURN(value.Prototype, sagaID)
	if err != nil {
		p.add("prototype %v", err)
	}
	storyID, _, err := parseTargetURN(value.Target, sagaID)
	if err != nil {
		p.add("%v", err)
	}
	if strings.TrimSpace(value.Rationale) == "" {
		p.add("rationale is required")
	}
	if (value.PrototypeRevision == "") == (value.PrototypeContentDigest == "") {
		p.add("exactly one prototype revision or content digest pin is required")
	}
	if value.PrototypeRevision != "" && prototypeID != "" {
		if _, err := parseRevisionURN(value.PrototypeRevision, sagaID, prototypeID); err != nil {
			p.add("prototype_revision %v", err)
		}
	}
	if value.PrototypeContentDigest != "" && !digestPattern.MatchString(value.PrototypeContentDigest) {
		p.add("prototype_content_digest must use sha256:<64 lowercase hex>")
	}
	if storyID != "" {
		if err := parseStoryRevision(value.StoryRevision, sagaID, storyID); err != nil {
			p.add("%v", err)
		}
	}
	validateSelector(&p, value.Selector)
	validateTime(&p, value.CreatedAt)
	validateRequestID(&p, value.RequestID)
	return p.err()
}

func validateSelector(p *validationErrors, value Selector) {
	switch value.Kind {
	case SelectorElement:
		if !livingid.ValidID(value.ElementID) {
			p.add("element selector requires a stable element_id")
		}
		if value.ExactText != "" || value.Region != nil || value.ProviderID != "" || value.DeepLink != "" {
			p.add("element selector contains fields for another selector kind")
		}
	case SelectorText:
		if strings.TrimSpace(value.ExactText) == "" {
			p.add("text selector requires exact_text")
		}
		if value.ElementID != "" || value.Region != nil || value.ProviderID != "" || value.DeepLink != "" {
			p.add("text selector contains fields for another selector kind")
		}
	case SelectorRegion:
		if value.Region == nil {
			p.add("region selector requires region")
		} else if !normalizedRegion(*value.Region) {
			p.add("region must be normalized within the unit square")
		}
		if value.ElementID != "" || value.ExactText != "" || value.ProviderID != "" || value.DeepLink != "" {
			p.add("region selector contains fields for another selector kind")
		}
	case SelectorProvider:
		if strings.TrimSpace(value.ProviderID) == "" && !absoluteHTTPURL(value.DeepLink) {
			p.add("provider selector requires provider_id or an absolute deep_link")
		}
		if value.ElementID != "" || value.ExactText != "" || value.Region != nil {
			p.add("provider selector contains fields for another selector kind")
		}
	default:
		p.add("selector kind must be element, text, region, or provider")
	}
}

func normalizedRegion(r Region) bool {
	return r.X >= 0 && r.Y >= 0 && r.Width > 0 && r.Height > 0 && r.X+r.Width <= 1 && r.Y+r.Height <= 1
}
func absoluteHTTPURL(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && parsed.Host != "" && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.User == nil
}
func originOfURL(value string) (string, bool) {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return "", false
	}
	return "https://" + strings.ToLower(parsed.Host), true
}

func declaredOrigin(value string) (string, bool) {
	origin, ok := originOfURL(value)
	return origin, ok && value == origin
}

func safeRelativePath(value string) bool {
	if value == "" || filepath.IsAbs(value) || strings.ContainsAny(value, "\\:\x00") {
		return false
	}
	clean := filepath.Clean(value)
	return clean != "." && clean != ".." && !strings.HasPrefix(clean, ".."+string(filepath.Separator)) && filepath.ToSlash(clean) == value
}

func validateCSS(data []byte) error {
	normalized, err := normalizeCSSForSecurity(data)
	if err != nil {
		return err
	}
	for _, pattern := range unsafeCSS {
		if pattern.MatchString(normalized) {
			return fmt.Errorf("stylesheet contains forbidden construct matching %q", pattern.String())
		}
	}
	for _, marker := range []string{"http://", "https://", "//", "image-set(", "src("} {
		if strings.Contains(normalized, marker) {
			return fmt.Errorf("stylesheet contains forbidden fetch construct %q", marker)
		}
	}
	return nil
}

// normalizeCSSForSecurity closes common tokenizer evasions before applying the
// intentionally small allow policy: comments are removed and CSS hexadecimal
// escapes are decoded. Ambiguous escapes fail closed.
func normalizeCSSForSecurity(data []byte) (string, error) {
	text := strings.ToLower(string(data))
	for {
		start := strings.Index(text, "/*")
		if start < 0 {
			break
		}
		end := strings.Index(text[start+2:], "*/")
		if end < 0 {
			return "", fmt.Errorf("stylesheet contains an unterminated comment")
		}
		text = text[:start] + text[start+2+end+2:]
	}
	var out strings.Builder
	for index := 0; index < len(text); {
		if text[index] != '\\' {
			out.WriteByte(text[index])
			index++
			continue
		}
		index++
		start := index
		for index < len(text) && index-start < 6 && ((text[index] >= '0' && text[index] <= '9') || (text[index] >= 'a' && text[index] <= 'f')) {
			index++
		}
		if start == index {
			return "", fmt.Errorf("stylesheet contains an ambiguous escape")
		}
		value, err := strconv.ParseInt(text[start:index], 16, 32)
		if err != nil || value == 0 {
			return "", fmt.Errorf("stylesheet contains an invalid escape")
		}
		out.WriteRune(rune(value))
		if index < len(text) && (text[index] == ' ' || text[index] == '\t' || text[index] == '\r' || text[index] == '\n' || text[index] == '\f') {
			index++
		}
	}
	return out.String(), nil
}

func readRepositoryStyle(repositoryRoot, path string) ([]byte, string, error) {
	if !safeRelativePath(path) || filepath.Ext(path) != ".css" {
		return nil, "", fmt.Errorf("stylesheet path must be a safe relative .css path")
	}
	root, err := filepath.Abs(repositoryRoot)
	if err != nil {
		return nil, "", err
	}
	rootInfo, err := os.Lstat(root)
	if err != nil {
		return nil, "", err
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 || !rootInfo.IsDir() {
		return nil, "", fmt.Errorf("repository root must be a real directory")
	}
	current := root
	for _, part := range strings.Split(filepath.FromSlash(path), string(filepath.Separator)) {
		current = filepath.Join(current, part)
		info, statErr := os.Lstat(current)
		if statErr != nil {
			return nil, "", statErr
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, "", fmt.Errorf("stylesheet path must not contain symlinks")
		}
	}
	info, err := os.Lstat(current)
	if err != nil {
		return nil, "", err
	}
	if !info.Mode().IsRegular() {
		return nil, "", fmt.Errorf("stylesheet must be a regular file")
	}
	if info.Size() > MaxHTMLFileBytes {
		return nil, "", fmt.Errorf("stylesheet exceeds %d bytes", MaxHTMLFileBytes)
	}
	data, err := os.ReadFile(current)
	if err != nil {
		return nil, "", err
	}
	if err := validateCSS(data); err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(data)
	return data, fmt.Sprintf("sha256:%x", sum), nil
}

// ReadPinnedStyle returns repository CSS only when the requested declaration
// is safe and still matches its SHA-256 pin. Viewers can use this as the sole
// style-serving boundary instead of opening arbitrary repository paths.
func ReadPinnedStyle(repositoryRoot string, style StyleSource) ([]byte, error) {
	var problems validationErrors
	validateStyle(&problems, "style", style)
	if err := problems.err(); err != nil {
		return nil, err
	}
	data, digest, err := readRepositoryStyle(repositoryRoot, style.Path)
	if err != nil {
		return nil, err
	}
	if digest != style.Digest {
		return nil, fmt.Errorf("stylesheet digest changed")
	}
	return data, nil
}

func validateRevisionGraph(prototype *Prototype, sagaID string) error {
	var p validationErrors
	values := map[string]Revision{}
	parents := map[string][]string{}
	for _, revision := range prototype.Revisions {
		if _, ok := values[revision.ID]; ok {
			p.add("revision id %q is duplicated", revision.ID)
		}
		values[revision.ID] = revision
	}
	for id, revision := range values {
		for _, parentURN := range revision.Parents {
			parentID, err := parseRevisionURN(parentURN, sagaID, prototype.Identity.ID)
			if err != nil {
				continue
			}
			if _, ok := values[parentID]; !ok {
				p.add("revision %q names missing parent %q", id, parentURN)
			} else {
				parents[id] = append(parents[id], parentID)
			}
		}
	}
	heads, roots, cycle := graphHeads(parents, mapKeys(values))
	if len(roots) != 1 {
		p.add("revision graph must have exactly one initial revision; found %d", len(roots))
	}
	if cycle {
		p.add("revision graph must be acyclic")
	}
	prototype.RevisionHeads = nil
	for _, id := range heads {
		urn, _ := RevisionURN(sagaID, prototype.Identity.ID, id)
		prototype.RevisionHeads = append(prototype.RevisionHeads, urn)
	}
	if len(heads) == 1 {
		v := values[heads[0]]
		prototype.CurrentRevision = &v
	}
	return p.err()
}

func graphHeads(parents map[string][]string, ids []string) (heads, roots []string, cycle bool) {
	parentSet := map[string]bool{}
	for _, id := range ids {
		if len(parents[id]) == 0 {
			roots = append(roots, id)
		}
		for _, parent := range parents[id] {
			parentSet[parent] = true
		}
	}
	for _, id := range ids {
		if !parentSet[id] {
			heads = append(heads, id)
		}
	}
	colors := map[string]uint8{}
	var visit func(string) bool
	visit = func(id string) bool {
		if colors[id] == 1 {
			return true
		}
		if colors[id] == 2 {
			return false
		}
		colors[id] = 1
		for _, p := range parents[id] {
			if visit(p) {
				return true
			}
		}
		colors[id] = 2
		return false
	}
	for _, id := range ids {
		cycle = cycle || visit(id)
	}
	sort.Strings(heads)
	sort.Strings(roots)
	return
}
func mapKeys[T any](values map[string]T) []string {
	r := make([]string, 0, len(values))
	for k := range values {
		r = append(r, k)
	}
	return r
}
func validateTime(p *validationErrors, value time.Time) {
	if value.IsZero() || value.Location() != time.UTC {
		p.add("timestamp must be a non-zero UTC RFC 3339 value")
	}
}
func validateRequestID(p *validationErrors, value string) {
	if value != "" && !livingid.ValidID(value) {
		p.add("request_id must be a stable identifier")
	}
}

// digestTree hashes path, size, and bytes for every regular file in lexical
// order. Symlinks and special files are rejected before their targets open.
func digestTree(root string) (string, error) {
	type item struct {
		path string
		size int64
	}
	var files []item
	var total int64
	entries := 0
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		entries++
		if entries > MaxHTMLFiles*2 {
			return fmt.Errorf("html package contains too many entries")
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("html package contains symlink %q", path)
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("html package contains non-regular file %q", path)
		}
		if info.Size() > MaxHTMLFileBytes {
			return fmt.Errorf("html file %q exceeds %d bytes", path, MaxHTMLFileBytes)
		}
		rel, err := filepath.Rel(root, path)
		if err != nil || !safeRelativePath(filepath.ToSlash(rel)) {
			return fmt.Errorf("html package contains unsafe path")
		}
		files = append(files, item{filepath.ToSlash(rel), info.Size()})
		total += info.Size()
		if len(files) > MaxHTMLFiles || total > MaxHTMLPackageBytes {
			return fmt.Errorf("html package exceeds bounded size")
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if len(files) == 0 {
		return "", fmt.Errorf("html package is empty")
	}
	sort.Slice(files, func(i, j int) bool { return files[i].path < files[j].path })
	h := sha256.New()
	for _, f := range files {
		io.WriteString(h, f.path)
		io.WriteString(h, "\x00")
		io.WriteString(h, fmt.Sprintf("%d", f.size))
		io.WriteString(h, "\x00")
		file, err := os.Open(filepath.Join(root, filepath.FromSlash(f.path)))
		if err != nil {
			return "", err
		}
		_, copyErr := io.Copy(h, file)
		closeErr := file.Close()
		if copyErr != nil {
			return "", copyErr
		}
		if closeErr != nil {
			return "", closeErr
		}
	}
	return fmt.Sprintf("sha256:%x", h.Sum(nil)), nil
}
