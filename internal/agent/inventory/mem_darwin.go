//go:build darwin

package inventory

import (
	"bufio"
	"os/exec"
	"strconv"
	"strings"

	"github.com/knot-infra/knot/pkg/protocol"
	"golang.org/x/sys/unix"
)

func readMemory() protocol.ComputeMemory {
	var mem protocol.ComputeMemory
	total, err := unix.SysctlUint64("hw.memsize")
	if err == nil {
		mem.TotalBytes = total
	}
	pageSize := uint64(unix.Getpagesize())
	freePages := sysctlUint("vm.page_free_count")
	inactive := sysctlUint("vm.page_inactive_count")
	speculative := sysctlUint("vm.page_speculative_count")
	purgeable := sysctlUint("vm.page_purgeable_count")
	avail := (freePages + inactive + speculative + purgeable) * pageSize
	if avail > mem.TotalBytes && mem.TotalBytes > 0 {
		avail = mem.TotalBytes
	}
	mem.AvailableBytes = avail
	if mem.TotalBytes >= mem.AvailableBytes {
		mem.UsedBytes = mem.TotalBytes - mem.AvailableBytes
	}
	if mem.AvailableBytes == 0 && mem.TotalBytes > 0 {
		if v := vmStatAvailable(pageSize); v > 0 {
			mem.AvailableBytes = v
			if mem.TotalBytes >= v {
				mem.UsedBytes = mem.TotalBytes - v
			}
		}
	}
	mem.UsagePercent = pct(mem.UsedBytes, mem.TotalBytes)
	return mem
}

func sysctlUint(name string) uint64 {
	n, err := unix.SysctlUint64(name)
	if err != nil {
		return 0
	}
	return n
}

func vmStatAvailable(pageSize uint64) uint64 {
	out, err := exec.Command("vm_stat").Output()
	if err != nil {
		return 0
	}
	var free, inactive, speculative uint64
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.Contains(line, "Pages free"):
			free = vmStatPages(line)
		case strings.Contains(line, "Pages inactive"):
			inactive = vmStatPages(line)
		case strings.Contains(line, "Pages speculative"):
			speculative = vmStatPages(line)
		}
	}
	return (free + inactive + speculative) * pageSize
}

func vmStatPages(line string) uint64 {
	i := strings.LastIndex(line, ":")
	if i < 0 {
		return 0
	}
	s := strings.TrimSpace(strings.TrimSuffix(line[i+1:], "."))
	s = strings.ReplaceAll(s, ",", "")
	n, _ := strconv.ParseUint(s, 10, 64)
	return n
}
