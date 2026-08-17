//go:build darwin

package inventory

import (
	"github.com/knot-infra/knot/pkg/protocol"
	"golang.org/x/sys/unix"
)

func readDisks() []protocol.ComputeDisk {
	n, err := unix.Getfsstat(nil, unix.MNT_NOWAIT)
	if err != nil || n <= 0 {
		return nil
	}
	buf := make([]unix.Statfs_t, n)
	n, err = unix.Getfsstat(buf, unix.MNT_NOWAIT)
	if err != nil {
		return nil
	}
	var out []protocol.ComputeDisk
	seen := map[string]bool{}
	for i := 0; i < n; i++ {
		s := buf[i]
		mount := unix.ByteSliceToString(s.Mntonname[:])
		fstype := unix.ByteSliceToString(s.Fstypename[:])
		name := unix.ByteSliceToString(s.Mntfromname[:])
		if seen[mount] {
			continue
		}
		bsize := uint64(s.Bsize)
		total := s.Blocks * bsize
		free := s.Bavail * bsize
		if skipMount(mount, fstype, total) {
			continue
		}
		seen[mount] = true
		used := uint64(0)
		if total >= free {
			used = total - free
		}
		out = append(out, protocol.ComputeDisk{
			Mount:      mount,
			Name:       name,
			FSType:     fstype,
			TotalBytes: total,
			FreeBytes:  free,
			UsedBytes:  used,
		})
	}
	return out
}
