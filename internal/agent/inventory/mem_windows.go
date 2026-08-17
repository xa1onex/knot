//go:build windows

package inventory

import (
	"unsafe"

	"github.com/knot-infra/knot/pkg/protocol"
	"golang.org/x/sys/windows"
)

type memoryStatusEx struct {
	length               uint32
	memoryLoad           uint32
	totalPhys            uint64
	availPhys            uint64
	totalPageFile        uint64
	availPageFile        uint64
	totalVirtual         uint64
	availVirtual         uint64
	availExtendedVirtual uint64
}

var procGlobalMemoryStatusEx = windows.NewLazySystemDLL("kernel32.dll").NewProc("GlobalMemoryStatusEx")

func readMemory() protocol.ComputeMemory {
	var mem protocol.ComputeMemory
	var st memoryStatusEx
	st.length = uint32(unsafe.Sizeof(st))
	r, _, _ := procGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&st)))
	if r == 0 {
		return mem
	}
	mem.TotalBytes = st.totalPhys
	mem.AvailableBytes = st.availPhys
	if mem.TotalBytes >= mem.AvailableBytes {
		mem.UsedBytes = mem.TotalBytes - mem.AvailableBytes
	}
	mem.UsagePercent = pct(mem.UsedBytes, mem.TotalBytes)
	return mem
}
