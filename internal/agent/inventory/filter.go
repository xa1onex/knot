package inventory

import "strings"

var skipFS = map[string]bool{
	"proc": true, "sysfs": true, "devtmpfs": true, "tmpfs": true, "cgroup": true, "cgroup2": true,
	"overlay": true, "squashfs": true, "ramfs": true, "autofs": true, "debugfs": true,
	"securityfs": true, "pstore": true, "bpf": true, "tracefs": true, "fusectl": true,
	"configfs": true, "hugetlbfs": true, "mqueue": true, "rpc_pipefs": true, "nsfs": true,
	"devfs": true, "devpts": true, "efivarfs": true, "binfmt_misc": true, "fuse.portal": true,
	"none": true, "iso9660": true,
}

func skipMount(mount, fstype string, total uint64) bool {
	fs := strings.ToLower(strings.TrimSpace(fstype))
	if skipFS[fs] || strings.HasPrefix(fs, "fuse.") && fs != "fuse.sshfs" {
		return true
	}
	if strings.HasPrefix(mount, "/proc") || strings.HasPrefix(mount, "/sys") || strings.HasPrefix(mount, "/dev") {
		return true
	}
	if strings.HasPrefix(mount, "/run") || strings.HasPrefix(mount, "/snap") {
		return true
	}
	if total > 0 && total < 64<<20 {
		return true
	}
	return false
}
