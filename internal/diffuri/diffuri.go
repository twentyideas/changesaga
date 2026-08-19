package diffuri

import (
	"fmt"
	"net/url"
	"strconv"
)

const Scheme = "saga-diff"

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
	if reference.Path != "" {
		query.Set("path", reference.Path)
	}
	if reference.OldPath != "" {
		query.Set("old_path", reference.OldPath)
	}
	if reference.NewPath != "" {
		query.Set("new_path", reference.NewPath)
	}
	return (&url.URL{Scheme: Scheme, Host: "v1", Path: "/event", RawQuery: query.Encode()}).String(), nil
}

func Parse(value string) (Reference, error) {
	parsed, err := url.Parse(value)
	if err != nil {
		return Reference{}, err
	}
	if parsed.Scheme != Scheme || parsed.Host != "v1" {
		return Reference{}, fmt.Errorf("URI must use %s://v1", Scheme)
	}
	query := parsed.Query()
	reference := Reference{
		Repository: query.Get("repository"), Base: query.Get("base"), Head: query.Get("head"),
		Path: query.Get("path"), Side: query.Get("side"), Event: query.Get("event"),
		OldPath: query.Get("old_path"), NewPath: query.Get("new_path"),
	}
	switch parsed.Path {
	case "/line":
		reference.Kind = "line"
		reference.Start, err = strconv.Atoi(query.Get("start"))
		if err != nil {
			return Reference{}, fmt.Errorf("invalid start line")
		}
		reference.End, err = strconv.Atoi(query.Get("end"))
		if err != nil {
			return Reference{}, fmt.Errorf("invalid end line")
		}
	case "/event":
		reference.Kind = "event"
	case "/file":
		reference.Kind = "file"
	default:
		return Reference{}, fmt.Errorf("URI path must be /line, /event, or /file")
	}
	if err := Validate(reference); err != nil {
		return Reference{}, err
	}
	return reference, nil
}

func Validate(reference Reference) error {
	repository, err := url.Parse(reference.Repository)
	if err != nil || !repository.IsAbs() || repository.Host == "" && repository.Scheme != "file" {
		return fmt.Errorf("repository must be an absolute URI")
	}
	if reference.Base == "" || reference.Head == "" {
		return fmt.Errorf("base and head identities are required")
	}
	switch reference.Kind {
	case "line":
		if reference.Path == "" || reference.Side != "old" && reference.Side != "new" || reference.Start < 1 || reference.End < reference.Start {
			return fmt.Errorf("line URI requires path, old/new side, and a valid range")
		}
	case "event":
		if reference.Event != "rename" && reference.Event != "mode" && reference.Event != "binary" {
			return fmt.Errorf("event URI requires rename, mode, or binary")
		}
		if reference.Event == "rename" && (reference.OldPath == "" || reference.NewPath == "") {
			return fmt.Errorf("rename URI requires old_path and new_path")
		}
		if reference.Event != "rename" && reference.Path == "" {
			return fmt.Errorf("mode and binary URIs require path")
		}
	case "file":
		if reference.Path == "" {
			return fmt.Errorf("file URI requires path")
		}
	default:
		return fmt.Errorf("kind must be line, event, or file")
	}
	return nil
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
	if selector.Path != "" && selector.Path != atom.Path {
		return false
	}
	if selector.OldPath != "" && selector.OldPath != atom.OldPath {
		return false
	}
	return selector.NewPath == "" || selector.NewPath == atom.NewPath
}
