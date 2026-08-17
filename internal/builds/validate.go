package builds

import (
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	imageRe    = regexp.MustCompile(`^[a-z0-9][a-z0-9._/-]{0,253}(:[a-zA-Z0-9._-]{1,128})?$`)
	branchRe   = regexp.MustCompile(`^[A-Za-z0-9._/-]{1,255}$`)
	revisionRe = regexp.MustCompile(`^[0-9a-fA-F]{7,64}$`)
	nameRe     = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)
	fakeGitRe  = regexp.MustCompile(`^knot-fake-git:[a-z0-9._:-]+$`)
)

func validateSourceURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fmt.Errorf("%w: url required", ErrValidation)
	}
	if strings.ContainsAny(raw, " \n\r\t") {
		return fmt.Errorf("%w: url must not contain whitespace", ErrValidation)
	}
	if fakeGitRe.MatchString(raw) {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%w: invalid url", ErrValidation)
	}
	if u.User != nil {
		return fmt.Errorf("%w: git credentials must use a vault secret, not the url", ErrValidation)
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return fmt.Errorf("%w: url must be https, http, or knot-fake-git", ErrValidation)
	}
	if u.Host == "" {
		return fmt.Errorf("%w: url host required", ErrValidation)
	}
	return nil
}

func validateRelPath(p, field string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", fmt.Errorf("%w: %s required", ErrValidation, field)
	}
	p = filepath.ToSlash(p)
	if filepath.IsAbs(p) || strings.HasPrefix(p, "/") {
		return "", fmt.Errorf("%w: %s must be a relative path", ErrValidation, field)
	}
	if strings.Contains(p, "..") {
		return "", fmt.Errorf("%w: %s must not contain ..", ErrValidation, field)
	}
	return p, nil
}

func validateImageTag(tag string) error {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return fmt.Errorf("%w: tag required", ErrValidation)
	}
	if !imageRe.MatchString(tag) {
		return fmt.Errorf("%w: invalid image tag", ErrValidation)
	}
	return nil
}

func validateRef(kind, v string) error {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	if kind == "revision" {
		if !revisionRe.MatchString(v) {
			return fmt.Errorf("%w: invalid revision", ErrValidation)
		}
		return nil
	}
	if !branchRe.MatchString(v) {
		return fmt.Errorf("%w: invalid %s", ErrValidation, kind)
	}
	return nil
}

func defaultNameFromURL(raw string) string {
	raw = strings.TrimSuffix(strings.TrimSpace(raw), ".git")
	if fakeGitRe.MatchString(raw) {
		return strings.TrimPrefix(raw, "knot-fake-git:")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "app"
	}
	base := filepath.Base(u.Path)
	base = strings.TrimSuffix(base, ".git")
	if base == "" || base == "." || base == "/" {
		return "app"
	}
	if nameRe.MatchString(base) {
		return base
	}
	return "app"
}

func secretRef(v string) string {
	return strings.TrimPrefix(strings.TrimSpace(v), "secret://")
}
