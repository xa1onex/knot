package compute

import (
	"fmt"
	"regexp"
	"strings"
)

var labelKeyRe = regexp.MustCompile(`^[a-z0-9_]{1,32}$`)

func ValidateUserLabels(m map[string]string) error {
	if len(m) > 16 {
		return fmt.Errorf("too many labels")
	}
	for k, v := range m {
		if !labelKeyRe.MatchString(strings.ToLower(strings.TrimSpace(k))) {
			return fmt.Errorf("invalid label key")
		}
		if len(v) > 64 || strings.ContainsRune(v, 0) {
			return fmt.Errorf("invalid label value")
		}
	}
	return nil
}

func LabelsFor(rec Record, user map[string]string) map[string]string {
	out := map[string]string{}
	osName := strings.ToLower(strings.TrimSpace(rec.OS))
	if osName != "" {
		out["os"] = osName
		switch {
		case strings.Contains(osName, "win"):
			out["windows"] = "true"
		case strings.Contains(osName, "linux"):
			out["linux"] = "true"
		case strings.Contains(osName, "darwin") || strings.Contains(osName, "mac"):
			out["darwin"] = "true"
		}
	}
	if rec.Arch != "" {
		out["arch"] = strings.ToLower(rec.Arch)
	}
	if rec.GPU != nil {
		n := 0
		for _, g := range *rec.GPU {
			if g.Available != nil && !*g.Available {
				continue
			}
			n++
		}
		if n > 0 {
			out["gpu"] = "true"
		}
	}
	for k, v := range user {
		k = strings.TrimSpace(strings.ToLower(k))
		if k == "" {
			continue
		}
		out[k] = strings.TrimSpace(v)
	}
	if out == nil {
		out = map[string]string{}
	}
	return out
}
