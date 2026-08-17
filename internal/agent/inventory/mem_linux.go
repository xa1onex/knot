//go:build linux

package inventory

import (
	"bufio"
	"os"
	"strings"

	"github.com/knot-infra/knot/pkg/protocol"
)

func readMemory() protocol.ComputeMemory {
	var mem protocol.ComputeMemory
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return mem
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	var totalKB, availKB uint64
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "MemTotal:"):
			totalKB = parseMeminfoKB(line)
		case strings.HasPrefix(line, "MemAvailable:"):
			availKB = parseMeminfoKB(line)
		}
	}
	mem.TotalBytes = totalKB * 1024
	mem.AvailableBytes = availKB * 1024
	if mem.TotalBytes >= mem.AvailableBytes {
		mem.UsedBytes = mem.TotalBytes - mem.AvailableBytes
	}
	mem.UsagePercent = pct(mem.UsedBytes, mem.TotalBytes)
	return mem
}

func parseMeminfoKB(line string) uint64 {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return 0
	}
	var n uint64
	for _, c := range fields[1] {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + uint64(c-'0')
	}
	return n
}
