package store

import (
	"net/url"
	"path"
	"strings"
)

// DeriveName turns a source into the store name used to address the spec. The
// indexer stores under this name when --name is absent, and spec_status compares
// it with the stored name to know whether the refresh command needs --name.
//
// It lives here so both sides share one derivation: a local copy in the indexer
// and another in the query layer is how a refresh command ends up renaming a
// spec.
func DeriveName(source string) string {
	trimmed := strings.TrimSpace(source)
	if trimmed == "" {
		return "spec"
	}

	var base string
	if IsURL(trimmed) {
		if u, err := url.Parse(trimmed); err == nil {
			base = path.Base(strings.TrimRight(u.Path, "/"))
			if base == "." || base == "/" || base == "" {
				base = u.Hostname()
			}
		}
	}
	if base == "" {
		base = path.Base(strings.ReplaceAll(trimmed, `\`, "/"))
	}

	if ext := path.Ext(base); ext != "" {
		switch strings.ToLower(ext) {
		case ".json", ".yaml", ".yml":
			base = strings.TrimSuffix(base, ext)
		}
	}

	base = strings.TrimSpace(base)
	if base == "" || base == "." || base == "/" {
		return "spec"
	}
	return base
}

// IsURL reports whether a source has to be read over HTTP. Only the indexer asks:
// a query addresses the store by name (S4a).
func IsURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	return (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}
