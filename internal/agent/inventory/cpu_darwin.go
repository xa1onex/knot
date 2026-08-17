//go:build darwin

package inventory

import (
	"encoding/binary"

	"golang.org/x/sys/unix"
)

func cpuUsagePercent() *float64 {
	return sampleCPU(readCPUTimes)
}

func readCPUTimes() (idle, total uint64, ok bool) {
	raw, err := unix.SysctlRaw("kern.cp_time")
	if err != nil || len(raw) < 8 {
		return 0, 0, false
	}
	n := len(raw) / 8
	if n < 4 {
		return 0, 0, false
	}
	vals := make([]uint64, n)
	for i := 0; i < n; i++ {
		vals[i] = binary.LittleEndian.Uint64(raw[i*8 : (i+1)*8])
		total += vals[i]
	}
	// CPUSTATES: user, nice, sys, idle, intr
	idle = vals[3]
	return idle, total, true
}
