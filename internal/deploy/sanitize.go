package deploy

import (
	"encoding/json"
	"regexp"
	"strings"
)

var secretKeyRe = regexp.MustCompile(`(?i)(secret|password|token|api[_-]?key|credential|private)`)

// RedactEnv returns a copy of env with sensitive values masked.
func RedactEnv(env map[string]string) map[string]string {
	if len(env) == 0 {
		return nil
	}
	out := make(map[string]string, len(env))
	for k, v := range env {
		if secretKeyRe.MatchString(k) {
			out[k] = "[redacted]"
		} else {
			out[k] = v
		}
	}
	return out
}

var userinfoRe = regexp.MustCompile(`(?i)(https?://)[^/@\s:]+:[^/@\s]+@`)

// SanitizeLogLine redacts common secret patterns in log text.
func SanitizeLogLine(line string) string {
	if line == "" {
		return line
	}
	line = userinfoRe.ReplaceAllString(line, "${1}[redacted]@")
	// key=value or key: value patterns
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
	return strings.Join(parts, " ")
}

// RedactSecrets applies SanitizeLogLine then replaces known secret values.
func RedactSecrets(line string, secrets []string) string {
	line = SanitizeLogLine(line)
	for _, s := range secrets {
		if s != "" && strings.Contains(line, s) {
			line = strings.ReplaceAll(line, s, "[redacted]")
		}
	}
	return line
}

func envJSON(env map[string]string) string {
	if len(env) == 0 {
		return "{}"
	}
	b, _ := json.Marshal(env)
	return string(b)
}

func parseEnvJSON(raw string) map[string]string {
	return ParseEnvJSON(raw)
}

// ParseEnvJSON decodes stored deployment env JSON.
func ParseEnvJSON(raw string) map[string]string {
	if raw == "" || raw == "{}" {
		return nil
	}
	var m map[string]string
	if json.Unmarshal([]byte(raw), &m) != nil {
		return nil
	}
	return m
}
