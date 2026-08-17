//go:build windows

package inventory

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modkernel32        = windows.NewLazySystemDLL("kernel32.dll")
	procGetSystemTimes = modkernel32.NewProc("GetSystemTimes")
)

func cpuUsagePercent() *float64 {
	return sampleCPU(readCPUTimes)
}

func readCPUTimes() (idle, total uint64, ok bool) {
	var idleT, kernelT, userT windows.Filetime
	r, _, _ := procGetSystemTimes.Call(
		uintptr(unsafe.Pointer(&idleT)),
		uintptr(unsafe.Pointer(&kernelT)),
		uintptr(unsafe.Pointer(&userT)),
	)
	if r == 0 {
		return 0, 0, false
	}
	idle = filetimeToUint(idleT)
	kernel := filetimeToUint(kernelT)
	user := filetimeToUint(userT)
	total = kernel + user
	return idle, total, true
}

func filetimeToUint(ft windows.Filetime) uint64 {
	return (uint64(ft.HighDateTime) << 32) | uint64(ft.LowDateTime)
}
