package oplogs

import (
	"regexp"
	"strings"
)

var (
	userinfoRe = regexp.MustCompile(`(?i)(https?://)[^/@\s:]+:[^/@\s]+@`)
	secretRe   = regexp.MustCompile(`(?i)(secret|password|token|api[_-]?key|credential|private)[=:][^\s,;]+`)
)

// Redact strips secret values so PASSWORD=, TOKEN=, SECRET= never persist in plaintext.
func Redact(line string) string {
	if line == "" {
		return line
	}
	line = userinfoRe.ReplaceAllString(line, "${1}[redacted]@")
	parts := strings.Split(line, " ")
	for i, p := range parts {
		lower := strings.ToLower(p)
		if strings.Contains(lower, "secret=") || strings.Contains(lower, "password=") ||
			strings.Contains(lower, "token=") || strings.Contains(lower, "api_key=") {
			if eq := strings.Index(p, "="); eq > 0 {
				parts[i] = p[:eq+1] + "[redacted]"
			}
		}
	}
	line = strings.Join(parts, " ")
	return secretRe.ReplaceAllStringFunc(line, func(m string) string {
		i := strings.IndexAny(m, "=:")
		if i < 0 {
			return "[redacted]"
		}
		return m[:i+1] + "[redacted]"
	})
}
