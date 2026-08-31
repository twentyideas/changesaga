// Package sagaref defines portable, revision-pinned references to targets in
// another Change Saga. It deliberately contains no checkout or Saga-storage
// code; availability is a resolution concern, not part of URI validity.
package sagaref

import (
	"fmt"
	"net"
	"net/url"
	"path"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/twentyideas/changesaga/internal/livingid"
)

const (
	Scheme  = "saga-ref"
	Version = "v1"
)

// Reference is the structured form of a saga-ref://v1/target URI.
// Repository, Revision, and TargetURN are immutable identities. TrackingBranch
// and ViewURL are optional refresh and navigation hints and never affect which
// target is resolved.
type Reference struct {
	Repository     string
	SagaPath       string
	SagaID         string
	Revision       string
	TargetURN      string
	TrackingBranch string
	ViewURL        string
}

var gitOID = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)

// Build validates reference and returns its canonical portable URI. Values
// with canonical equivalents, such as an upper-case Git OID or a repository
// URL containing a default port, are normalized before validation.
func Build(reference Reference) (string, error) {
	repository, err := CanonicalRepository(reference.Repository)
	if err != nil {
		return "", err
	}
	reference.Repository = repository
	reference.Revision = strings.ToLower(reference.Revision)
	if reference.ViewURL != "" {
		viewURL, err := CanonicalViewURL(reference.ViewURL)
		if err != nil {
			return "", err
		}
		reference.ViewURL = viewURL
	}
	if err := Validate(reference); err != nil {
		return "", err
	}

	query := url.Values{}
	query.Set("repository", reference.Repository)
	query.Set("revision", reference.Revision)
	query.Set("saga_id", reference.SagaID)
	query.Set("saga_path", reference.SagaPath)
	query.Set("target", reference.TargetURN)
	if reference.TrackingBranch != "" {
		query.Set("tracking_branch", reference.TrackingBranch)
	}
	if reference.ViewURL != "" {
		query.Set("view_url", reference.ViewURL)
	}
	return (&url.URL{Scheme: Scheme, Host: Version, Path: "/target", RawQuery: query.Encode()}).String(), nil
}

// Parse accepts only the canonical saga-ref://v1 representation.
func Parse(value string) (Reference, error) {
	parsed, err := url.Parse(value)
	if err != nil {
		return Reference{}, fmt.Errorf("parse Saga reference: %w", err)
	}
	if parsed.Scheme != Scheme || parsed.Host != Version || parsed.Path != "/target" || parsed.User != nil || parsed.Fragment != "" {
		return Reference{}, fmt.Errorf("URI must use %s://%s/target without userinfo or fragment", Scheme, Version)
	}
	query, err := url.ParseQuery(parsed.RawQuery)
	if err != nil {
		return Reference{}, fmt.Errorf("parse Saga reference query: %w", err)
	}
	allowed := map[string]bool{
		"repository": true, "saga_path": true, "saga_id": true,
		"revision": true, "target": true, "tracking_branch": true,
		"view_url": true,
	}
	for key, values := range query {
		if !allowed[key] {
			return Reference{}, fmt.Errorf("unknown query parameter %q", key)
		}
		if len(values) != 1 {
			return Reference{}, fmt.Errorf("query parameter %q must appear exactly once", key)
		}
	}
	getRequired := func(key string) (string, error) {
		values, ok := query[key]
		if !ok || len(values) != 1 || values[0] == "" {
			return "", fmt.Errorf("query parameter %q is required exactly once", key)
		}
		return values[0], nil
	}
	reference := Reference{}
	if reference.Repository, err = getRequired("repository"); err != nil {
		return Reference{}, err
	}
	if reference.SagaPath, err = getRequired("saga_path"); err != nil {
		return Reference{}, err
	}
	if reference.SagaID, err = getRequired("saga_id"); err != nil {
		return Reference{}, err
	}
	if reference.Revision, err = getRequired("revision"); err != nil {
		return Reference{}, err
	}
	if reference.TargetURN, err = getRequired("target"); err != nil {
		return Reference{}, err
	}
	if values := query["tracking_branch"]; len(values) == 1 {
		if values[0] == "" {
			return Reference{}, fmt.Errorf("query parameter %q cannot be empty", "tracking_branch")
		}
		reference.TrackingBranch = values[0]
	}
	if values := query["view_url"]; len(values) == 1 {
		if values[0] == "" {
			return Reference{}, fmt.Errorf("query parameter %q cannot be empty", "view_url")
		}
		reference.ViewURL = values[0]
	}
	if err := Validate(reference); err != nil {
		return Reference{}, err
	}
	canonical, err := Build(reference)
	if err != nil {
		return Reference{}, err
	}
	if canonical != value {
		return Reference{}, fmt.Errorf("Saga reference is not canonical; canonical form is %s", canonical)
	}
	return reference, nil
}

// Validate checks the canonical structured representation. It performs no
// filesystem or network access.
func Validate(reference Reference) error {
	repository, err := CanonicalRepository(reference.Repository)
	if err != nil {
		return err
	}
	if repository != reference.Repository {
		return fmt.Errorf("repository URI is not canonical")
	}
	if err := validateSagaPath(reference.SagaPath); err != nil {
		return err
	}
	if !livingid.ValidID(reference.SagaID) {
		return fmt.Errorf("Saga ID is not a stable identifier")
	}
	if !gitOID.MatchString(reference.Revision) {
		return fmt.Errorf("revision must be a lower-case 40- or 64-hex immutable Git object ID")
	}
	target, err := ParseTarget(reference.TargetURN)
	if err != nil {
		return err
	}
	if target.SagaID != reference.SagaID {
		return fmt.Errorf("target URN Saga ID %q does not match reference Saga ID %q", target.SagaID, reference.SagaID)
	}
	if reference.TrackingBranch != "" && !validBranch(reference.TrackingBranch) {
		return fmt.Errorf("tracking branch is not a valid canonical Git branch name")
	}
	if reference.ViewURL != "" {
		viewURL, err := CanonicalViewURL(reference.ViewURL)
		if err != nil {
			return err
		}
		if viewURL != reference.ViewURL {
			return fmt.Errorf("view URL is not canonical")
		}
	}
	return nil
}

func validateSagaPath(value string) error {
	if value == "" || value == "." || value == ".." || strings.HasPrefix(value, "../") || strings.HasPrefix(value, "/") ||
		!utf8.ValidString(value) || strings.ContainsRune(value, 0) || strings.Contains(value, `\`) || path.Clean(value) != value {
		return fmt.Errorf("Saga path must be a normalized repository-relative slash path")
	}
	if len(value) >= 2 && ((value[0] >= 'A' && value[0] <= 'Z') || (value[0] >= 'a' && value[0] <= 'z')) && value[1] == ':' {
		return fmt.Errorf("Saga path must be repository-relative")
	}
	if !strings.HasSuffix(path.Base(value), ".saga") {
		return fmt.Errorf("Saga path must name a .saga directory")
	}
	return nil
}

func validBranch(value string) bool {
	if value == "@" || strings.HasPrefix(value, "-") || strings.HasPrefix(value, "refs/heads/") ||
		strings.HasPrefix(value, "/") || strings.HasSuffix(value, "/") || strings.HasSuffix(value, ".") ||
		strings.Contains(value, "..") || strings.Contains(value, "@{") || strings.Contains(value, "//") || !utf8.ValidString(value) {
		return false
	}
	for _, component := range strings.Split(value, "/") {
		if component == "" || strings.HasPrefix(component, ".") || strings.HasSuffix(component, ".lock") {
			return false
		}
	}
	for _, r := range value {
		if r <= ' ' || r == 0x7f || strings.ContainsRune(`~^:?*[\`, r) {
			return false
		}
	}
	return true
}

// CanonicalRepository returns the portable repository identity. Credentials,
// query parameters, and fragments are never part of that identity.
func CanonicalRepository(value string) (string, error) {
	value = strings.TrimSpace(value)
	parsed, err := url.Parse(value)
	if err != nil || !parsed.IsAbs() {
		return "", fmt.Errorf("repository must be an absolute URI")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("repository URI cannot contain a query or fragment")
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.User = nil
	if parsed.Scheme == "file" {
		if parsed.Opaque != "" || parsed.Path == "" || !strings.HasPrefix(parsed.Path, "/") {
			return "", fmt.Errorf("file repository URI must have an absolute path")
		}
		parsed.Host = strings.ToLower(parsed.Host)
		parsed.Path = path.Clean(parsed.Path)
		parsed.RawPath = ""
		return parsed.String(), nil
	}
	if parsed.Opaque != "" || parsed.Host == "" {
		return "", fmt.Errorf("repository must be an absolute URI with a host")
	}
	parsed.Host = canonicalHost(parsed)
	cleanPath := path.Clean("/" + strings.TrimPrefix(parsed.Path, "/"))
	if cleanPath != "/" {
		cleanPath = strings.TrimSuffix(cleanPath, "/")
	}
	parsed.Path = cleanPath
	parsed.RawPath = ""
	return parsed.String(), nil
}

// CanonicalViewURL validates and canonicalizes a browser navigation hint.
// Unlike repository identity, a view URL may retain a query and fragment.
func CanonicalViewURL(value string) (string, error) {
	if value != strings.TrimSpace(value) || !utf8.ValidString(value) {
		return "", fmt.Errorf("view URL must be a canonical absolute HTTP(S) URL")
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Opaque != "" || parsed.Host == "" || parsed.User != nil ||
		(parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", fmt.Errorf("view URL must be a canonical absolute HTTP(S) URL without userinfo")
	}
	parsed.Host = canonicalHost(parsed)
	if parsed.Path == "" {
		parsed.Path = "/"
	}
	parsed.RawPath = ""
	return parsed.String(), nil
}

func canonicalHost(parsed *url.URL) string {
	host := strings.ToLower(parsed.Hostname())
	if port := parsed.Port(); port != "" {
		if !(parsed.Scheme == "https" && port == "443" || parsed.Scheme == "http" && port == "80" || parsed.Scheme == "ssh" && port == "22") {
			return net.JoinHostPort(host, port)
		}
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return host
}
