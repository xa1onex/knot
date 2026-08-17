//go:build windows

package inventory

import (
	"unsafe"

	"github.com/knot-infra/knot/pkg/protocol"
	"golang.org/x/sys/windows"
)

var (
	modkernel32Disk             = windows.NewLazySystemDLL("kernel32.dll")
	procGetLogicalDriveStringsW = modkernel32Disk.NewProc("GetLogicalDriveStringsW")
	procGetDiskFreeSpaceExW     = modkernel32Disk.NewProc("GetDiskFreeSpaceExW")
	procGetDriveTypeW           = modkernel32Disk.NewProc("GetDriveTypeW")
)

func readDisks() []protocol.ComputeDisk {
	buf := make([]uint16, 256)
	n, _, _ := procGetLogicalDriveStringsW.Call(uintptr(len(buf)), uintptr(unsafe.Pointer(&buf[0])))
	if n == 0 {
		return nil
	}
	var out []protocol.ComputeDisk
	start := 0
	for i, c := range buf {
		if c != 0 {
			continue
		}
		if i == start {
			break
		}
		root := windows.UTF16ToString(buf[start:i])
		start = i + 1
		if root == "" {
			continue
		}
		dt, _, _ := procGetDriveTypeW.Call(uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(root))))
		// DRIVE_FIXED=3, DRIVE_REMOVABLE=2, DRIVE_REMOTE=4
		if dt != 2 && dt != 3 && dt != 4 {
			continue
		}
		var freeAvail, total, free uint64
		r, _, _ := procGetDiskFreeSpaceExW.Call(
			uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(root))),
			uintptr(unsafe.Pointer(&freeAvail)),
			uintptr(unsafe.Pointer(&total)),
			uintptr(unsafe.Pointer(&free)),
		)
		if r == 0 || skipMount(root, "ntfs", total) {
			continue
		}
		used := uint64(0)
		if total >= freeAvail {
			used = total - freeAvail
		}
		out = append(out, protocol.ComputeDisk{
			Mount:      stringsTrimSlash(root),
			FSType:     "ntfs",
			TotalBytes: total,
			FreeBytes:  freeAvail,
			UsedBytes:  used,
		})
	}
	return out
}

func stringsTrimSlash(s string) string {
	if len(s) > 0 && (s[len(s)-1] == '\\' || s[len(s)-1] == '/') {
		return s[:len(s)-1]
	}
	return s
}
