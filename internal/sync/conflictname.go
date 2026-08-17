package syncjob

import (
	"fmt"
	"path"
	"regexp"
	"strings"
	"time"
	"unicode"
)

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// ConflictCopyRelPath builds a deterministic keep_both name for the preserved side.
// Example: config.json + "Home PC" + mtime → config.conflict-home-pc-20260817-2231.json
// Never returns the original path; caller must still ensure uniqueness via UniqueConflictCopyRelPath.
func ConflictCopyRelPath(relPath, deviceName, mtime string) string {
	dir := path.Dir(relPath)
	base := path.Base(relPath)
	ext := path.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	if stem == "" {
		stem = "file"
	}
	slug := slugDevice(deviceName)
	if slug == "" {
		slug = "node"
	}
	stamp := conflictStamp(mtime)
	name := fmt.Sprintf("%s.conflict-%s-%s%s", stem, slug, stamp, ext)
	if dir == "." || dir == "" {
		return name
	}
	return path.Join(dir, name)
}

func slugDevice(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		} else if unicode.IsSpace(r) || r == '-' || r == '_' {
			b.WriteByte('-')
		}
	}
	out := nonSlug.ReplaceAllString(b.String(), "-")
	out = strings.Trim(out, "-")
	if len(out) > 32 {
		out = out[:32]
		out = strings.Trim(out, "-")
	}
	return out
}

func conflictStamp(mtime string) string {
	if t, err := time.Parse(time.RFC3339Nano, mtime); err == nil {
		return t.UTC().Format("20060102-1504")
	}
	if t, err := time.Parse(time.RFC3339, mtime); err == nil {
		return t.UTC().Format("20060102-1504")
	}
	return time.Now().UTC().Format("20060102-1504")
}

// UniqueConflictCopyRelPath appends -2, -3, … until exists returns false.
func UniqueConflictCopyRelPath(relPath, deviceName, mtime string, exists func(rel string) bool) string {
	base := ConflictCopyRelPath(relPath, deviceName, mtime)
	if !exists(base) {
		return base
	}
	dir := path.Dir(base)
	filename := path.Base(base)
	ext := path.Ext(filename)
	stem := strings.TrimSuffix(filename, ext)
	for n := 2; n < 1000; n++ {
		cand := fmt.Sprintf("%s-%d%s", stem, n, ext)
		if dir != "." && dir != "" {
			cand = path.Join(dir, cand)
		}
		if !exists(cand) {
			return cand
		}
	}
	return fmt.Sprintf("%s-%d%s", stem, time.Now().UnixNano(), ext)
}
