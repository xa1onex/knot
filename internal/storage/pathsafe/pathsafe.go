// Package pathsafe validates storage-relative paths against traversal and
// platform-specific escape tricks (separators, drive letters, UNC, symlinks).
package pathsafe

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

var (
	ErrEmpty   = errors.New("path required")
	ErrEscape  = errors.New("path escapes storage root")
	ErrInvalid = errors.New("invalid path")
)

// CanonicalRel normalizes a client-supplied relative path to slash-separated
// form without leading slash. Rejects absolute paths, drive letters, UNC,
// null bytes, and ".." segments.
func CanonicalRel(p string) (string, error) {
	if p == "" {
		return "", ErrEmpty
	}
	if strings.ContainsRune(p, 0) {
		return "", ErrInvalid
	}
	// Unify separators before any path logic.
	p = strings.ReplaceAll(p, `\`, `/`)
	p = strings.TrimSpace(p)
	if p == "" {
		return "", ErrEmpty
	}
	// Absolute / UNC / Windows drive
	if strings.HasPrefix(p, "/") {
		return "", ErrEscape
	}
	if len(p) >= 2 && p[1] == ':' {
		return "", ErrEscape
	}

	// Reject ".." before Clean — path.Clean("/../x") collapses to "/x".
	parts := strings.Split(p, "/")
	for _, seg := range parts {
		if seg == ".." {
			return "", ErrEscape
		}
	}

	cleaned := path.Clean(p)
	if cleaned == "." {
		return "", nil // storage root
	}
	if strings.HasPrefix(cleaned, "/") {
		return "", ErrEscape
	}
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", ErrEscape
	}
	for _, seg := range strings.Split(cleaned, "/") {
		if seg == ".." || seg == "" {
			return "", ErrEscape
		}
	}
	return cleaned, nil
}

// ResolveUnderRoot joins root + rel and ensures the result stays inside root
// even after cleaning and symlink evaluation of existing parents.
func ResolveUnderRoot(root, rel string) (string, error) {
	canon, err := CanonicalRel(rel)
	if err != nil {
		return "", err
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	// EvalSymlinks on root when it exists so root itself cannot be a link out.
	if st, err := os.Lstat(absRoot); err == nil {
		if st.Mode()&os.ModeSymlink != 0 {
			resolved, err := filepath.EvalSymlinks(absRoot)
			if err != nil {
				return "", err
			}
			absRoot = resolved
		}
	}
	absRoot, err = filepath.Abs(absRoot)
	if err != nil {
		return "", err
	}

	target := absRoot
	if canon != "" {
		target = filepath.Join(absRoot, filepath.FromSlash(canon))
	}
	target = filepath.Clean(target)

	if err := ensureInside(absRoot, target); err != nil {
		return "", err
	}

	// Walk existing ancestors; reject symlink that escapes root.
	cur := target
	for {
		fi, err := os.Lstat(cur)
		if err != nil {
			if os.IsNotExist(err) {
				parent := filepath.Dir(cur)
				if parent == cur {
					break
				}
				cur = parent
				if err := ensureInside(absRoot, cur); err != nil {
					return "", err
				}
				continue
			}
			return "", err
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			resolved, err := filepath.EvalSymlinks(cur)
			if err != nil {
				return "", fmt.Errorf("%w: %v", ErrEscape, err)
			}
			resolved, err = filepath.Abs(resolved)
			if err != nil {
				return "", err
			}
			if err := ensureInside(absRoot, resolved); err != nil {
				return "", err
			}
		}
		parent := filepath.Dir(cur)
		if parent == cur || !strings.HasPrefix(parent, absRoot) {
			break
		}
		if cur == absRoot {
			break
		}
		cur = parent
	}
	return target, nil
}

func ensureInside(root, candidate string) error {
	rel, err := filepath.Rel(root, candidate)
	if err != nil {
		return ErrEscape
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return ErrEscape
	}
	return nil
}

// DefaultDirs are created under a new storage root.
var DefaultDirs = []string{"photos", "projects", "backups", "shared"}
