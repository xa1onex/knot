//go:build !linux && !darwin && !windows

package inventory

func cpuUsagePercent() *float64 { return nil }
