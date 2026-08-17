//go:build linux

package inventory

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

func cpuUsagePercent() *float64 {
	return sampleCPU(readCPUTimes)
}

func readCPUTimes() (idle, total uint64, ok bool) {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return 0, 0, false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	if !sc.Scan() {
		return 0, 0, false
	}
	fields := strings.Fields(sc.Text())
	if len(fields) < 5 || fields[0] != "cpu" {
		return 0, 0, false
	}
	var vals []uint64
	for _, tok := range fields[1:] {
		n, err := strconv.ParseUint(tok, 10, 64)
		if err != nil {
			return 0, 0, false
		}
		vals = append(vals, n)
		total += n
	}
	if len(vals) > 3 {
		idle = vals[3]
	}
	if len(vals) > 4 {
		idle += vals[4] // iowait
	}
	return idle, total, true
}
