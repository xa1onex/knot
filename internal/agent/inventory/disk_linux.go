//go:build linux

package inventory

import (
	"bufio"
	"os"
	"strings"

	"github.com/knot-infra/knot/pkg/protocol"
	"golang.org/x/sys/unix"
)

func readDisks() []protocol.ComputeDisk {
	f, err := os.Open("/proc/mounts")
	if err != nil {
		return nil
	}
	defer f.Close()
	seen := map[string]bool{}
	var out []protocol.ComputeDisk
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 3 {
			continue
		}
		device, mount, fstype := fields[0], fields[1], fields[2]
		if seen[mount] {
			continue
		}
		var st unix.Statfs_t
		if err := unix.Statfs(mount, &st); err != nil {
			continue
		}
		bsize := uint64(st.Bsize)
		total := st.Blocks * bsize
		free := st.Bavail * bsize
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
			Name:       device,
			FSType:     fstype,
			TotalBytes: total,
			FreeBytes:  free,
			UsedBytes:  used,
		})
	}
	return out
}
