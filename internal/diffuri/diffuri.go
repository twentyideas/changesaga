package diffuri

import (
	"fmt"
	"net"
	"net/url"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"unicode/utf8"
)

const Scheme = "saga-diff"

var eventKinds = map[string]bool{
	"add": true, "delete": true, "type-change": true,
	"rename": true, "mode": true, "binary": true, "modify": true,
}

type Reference struct {
	Repository string
	Base       string
	Head       string
	Kind       string
	Path       string
	Side       string
	Start      int
	End        int
	Event      string
	OldPath    string
	NewPath    string
}

func Build(reference Reference) (string, error) {
	repository, err := CanonicalRepository(reference.Repository)
	if err != nil {
		return "", err
	}
	reference.Repository = repository
	if err := Validate(reference); err != nil {
		return "", err
	}
	query := url.Values{}
	query.Set("repository", reference.Repository)
	query.Set("base", reference.Base)
	query.Set("head", reference.Head)
	if reference.Kind == "line" {
		query.Set("path", reference.Path)
		query.Set("side", reference.Side)
		query.Set("start", strconv.Itoa(reference.Start))
		query.Set("end", strconv.Itoa(reference.End))
		return (&url.URL{Scheme: Scheme, Host: "v1", Path: "/line", RawQuery: query.Encode()}).String(), nil
	}
	if reference.Kind == "file" {
		query.Set("path", reference.Path)
		return (&url.URL{Scheme: Scheme, Host: "v1", Path: "/file", RawQuery: query.Encode()}).String(), nil
	}
	query.Set("event", reference.Event)
	if reference.Event == "rename" {
		query.Set("old_path", reference.OldPath)
		query.Set("new_path", reference.NewPath)
	} else {
		query.Set("path", reference.Path)
	}
	return (&url.URL{Scheme: Scheme, Host: "v1", Path: "/event", RawQuery: query.Encode()}).String(), nil
}

func Parse(value string) (Reference, error) {
	parsed, err := url.Parse(value)
	if err != nil {
		return Reference{}, fmt.Errorf("parse diff URI: %w", err)
	}
	if parsed.Scheme != Scheme || parsed.Host != "v1" || parsed.User != nil || parsed.Fragment != "" {
		return Reference{}, fmt.Errorf("URI must use %s://v1 without userinfo or fragment", Scheme)
	}
	query, err := url.ParseQuery(parsed.RawQuery)
	if err != nil {
		return Reference{}, fmt.Errorf("parse diff URI query: %w", err)
	}
	allowed := map[string]bool{"repository": true, "base": true, "head": true}
	reference := Reference{}
	switch parsed.Path {
	case "/line":
		reference.Kind = "line"
		for _, key := range []string{"path", "side", "start", "end"} {
			allowed[key] = true
		}
	case "/event":
		reference.Kind = "event"
		for _, key := range []string{"event", "path", "old_path", "new_path"} {
			allowed[key] = true
		}
	case "/file":
		reference.Kind = "file"
		allowed["path"] = true
	default:
		return Reference{}, fmt.Errorf("URI path must be /line, /event, or /file")
	}
	for key, values := range query {
		if !allowed[key] {
			return Reference{}, fmt.Errorf("unknown query parameter %q", key)
		}
		if len(values) != 1 {
			return Reference{}, fmt.Errorf("query parameter %q must appear exactly once", key)
		}
	}
	get := func(key string) (string, error) {
		values, ok := query[key]
		if !ok || len(values) != 1 || values[0] == "" {
			return "", fmt.Errorf("query parameter %q is required exactly once", key)
		}
		return values[0], nil
	}
	if reference.Repository, err = get("repository"); err != nil {
		return Reference{}, err
	}
	if reference.Base, err = get("base"); err != nil {
		return Reference{}, err
	}
	if reference.Head, err = get("head"); err != nil {
		return Reference{}, err
	}
	if reference.Kind == "line" {
		if reference.Path, err = get("path"); err != nil {
			return Reference{}, err
		}
		if reference.Side, err = get("side"); err != nil {
			return Reference{}, err
		}
		start, startErr := get("start")
		if startErr != nil {
			return Reference{}, startErr
		}
		reference.Start, err = strconv.Atoi(start)
		if err != nil {
			return Reference{}, fmt.Errorf("invalid start line")
		}
		end, endErr := get("end")
		if endErr != nil {
			return Reference{}, endErr
		}
		reference.End, err = strconv.Atoi(end)
		if err != nil {
			return Reference{}, fmt.Errorf("invalid end line")
		}
	} else if reference.Kind == "event" {
		if reference.Event, err = get("event"); err != nil {
			return Reference{}, err
		}
		if values := query["path"]; len(values) == 1 {
			reference.Path = values[0]
		}
		if values := query["old_path"]; len(values) == 1 {
			reference.OldPath = values[0]
		}
		if values := query["new_path"]; len(values) == 1 {
			reference.NewPath = values[0]
		}
	} else {
		if reference.Path, err = get("path"); err != nil {
			return Reference{}, err
		}
	}
	if err := Validate(reference); err != nil {
		return Reference{}, err
	}
	canonical, err := Build(reference)
	if err != nil {
		return Reference{}, err
	}
	if value != canonical {
		return Reference{}, fmt.Errorf("diff URI is not canonical; canonical form is %s", canonical)
	}
	return reference, nil
}

func Validate(reference Reference) error {
	repository, err := CanonicalRepository(reference.Repository)
	if err != nil {
		return err
	}
	if repository != reference.Repository {
		return fmt.Errorf("repository URI is not canonical")
	}
	if reference.Base == "" || reference.Head == "" {
		return fmt.Errorf("base and head identities are required")
	}
	switch reference.Kind {
	case "line":
		if validatePath(reference.Path) != nil || reference.Side != "old" && reference.Side != "new" || reference.Start < 1 || reference.End < reference.Start {
			return fmt.Errorf("line URI requires path, old/new side, and a valid range")
		}
		if reference.Event != "" || reference.OldPath != "" || reference.NewPath != "" {
			return fmt.Errorf("line URI cannot contain event parameters")
		}
	case "event":
		if !eventKinds[reference.Event] {
			return fmt.Errorf("event URI requires add, delete, type-change, rename, mode, binary, or modify")
		}
		if reference.Event == "rename" {
			if reference.Path != "" || validatePath(reference.OldPath) != nil || validatePath(reference.NewPath) != nil {
				return fmt.Errorf("rename URI requires exactly old_path and new_path")
			}
		} else if validatePath(reference.Path) != nil || reference.OldPath != "" || reference.NewPath != "" {
			return fmt.Errorf("%s URI requires exactly path", reference.Event)
		}
	case "file":
		if validatePath(reference.Path) != nil {
			return fmt.Errorf("file URI requires path")
		}
		if reference.Side != "" || reference.Start != 0 || reference.End != 0 || reference.Event != "" || reference.OldPath != "" || reference.NewPath != "" {
			return fmt.Errorf("file URI cannot contain line or event parameters")
		}
	default:
		return fmt.Errorf("kind must be line, event, or file")
	}
	return nil
}

func validatePath(value string) error {
	if value == "" || value == "." || value == ".." || strings.HasPrefix(value, "../") || !utf8.ValidString(value) || strings.ContainsRune(value, 0) || strings.HasPrefix(value, "/") || path.Clean(value) != value {
		return fmt.Errorf("path must be a normalized repository-relative slash path")
	}
	if len(value) >= 2 && ((value[0] >= 'A' && value[0] <= 'Z') || (value[0] >= 'a' && value[0] <= 'z')) && value[1] == ':' {
		return fmt.Errorf("path must be repository-relative")
	}
	return nil
}

// CanonicalRepository returns the portable identity persisted in manifests and
// diff URIs. Authentication userinfo is deliberately discarded.
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
	if parsed.Opaque != "" {
		return "", fmt.Errorf("repository must be an absolute URI with a host")
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("repository must be an absolute URI with a host")
	}
	host := strings.ToLower(parsed.Hostname())
	if port := parsed.Port(); port != "" {
		if !(parsed.Scheme == "https" && port == "443" || parsed.Scheme == "http" && port == "80" || parsed.Scheme == "ssh" && port == "22") {
			host = net.JoinHostPort(host, port)
		}
	} else if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	parsed.Host = host
	cleanPath := path.Clean("/" + strings.TrimPrefix(parsed.Path, "/"))
	if cleanPath != "/" {
		cleanPath = strings.TrimSuffix(cleanPath, "/")
	}
	parsed.Path = cleanPath
	parsed.RawPath = ""
	return parsed.String(), nil
}

// FileRepository constructs a canonical file repository URI for a local path.
func FileRepository(value string) (string, error) {
	abs, err := filepath.Abs(value)
	if err != nil {
		return "", err
	}
	slash := filepath.ToSlash(filepath.Clean(abs))
	host := ""
	if runtime.GOOS == "windows" {
		if strings.HasPrefix(slash, "//") {
			parts := strings.SplitN(strings.TrimPrefix(slash, "//"), "/", 2)
			host = parts[0]
			slash = "/"
			if len(parts) == 2 {
				slash += parts[1]
			}
		} else if !strings.HasPrefix(slash, "/") {
			slash = "/" + slash
		}
	}
	return CanonicalRepository((&url.URL{Scheme: "file", Host: host, Path: slash}).String())
}

// RepositoryFilePath resolves a canonical file repository URI on this host.
func RepositoryFilePath(value string) (string, error) {
	canonical, err := CanonicalRepository(value)
	if err != nil {
		return "", err
	}
	parsed, _ := url.Parse(canonical)
	if parsed.Scheme != "file" {
		return "", fmt.Errorf("repository URI is not file://")
	}
	slash := parsed.Path
	if runtime.GOOS == "windows" {
		if parsed.Host != "" {
			slash = "//" + parsed.Host + slash
		} else if len(slash) >= 3 && slash[0] == '/' && slash[2] == ':' {
			slash = slash[1:]
		}
	} else if parsed.Host != "" {
		slash = "//" + parsed.Host + slash
	}
	local := filepath.Clean(filepath.FromSlash(slash))
	if !filepath.IsAbs(local) {
		return "", fmt.Errorf("file repository URI does not resolve to an absolute local path")
	}
	return local, nil
}

func Matches(selector, atom Reference) bool {
	if selector.Repository != atom.Repository || selector.Base != atom.Base || selector.Head != atom.Head || selector.Kind != atom.Kind {
		return false
	}
	if selector.Kind == "line" {
		return selector.Path == atom.Path && selector.Side == atom.Side && atom.Start >= selector.Start && atom.End <= selector.End
	}
	if selector.Kind == "file" {
		return selector.Path == atom.Path
	}
	if selector.Event != atom.Event {
		return false
	}
	if selector.Event == "rename" {
		return selector.OldPath == atom.OldPath && selector.NewPath == atom.NewPath
	}
	return selector.Path == atom.Path
}
