package offline

import "time"

const (
	BackoffStart = time.Second
	BackoffCap   = 30 * time.Second
)

// NextBackoff doubles prev (or starts at BackoffStart), capped at BackoffCap.
func NextBackoff(prev time.Duration) time.Duration {
	if prev <= 0 {
		return BackoffStart
	}
	next := prev * 2
	if next > BackoffCap {
		return BackoffCap
	}
	return next
}
