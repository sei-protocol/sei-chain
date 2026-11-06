package node

import (
	"io/fs"
	"os"
	"path"
	"slices"
	"strconv"
	"strings"
)

// cgroupUnlimited is the smallest value treated as no limit; cgroup v1 reports an unlimited
// cgroup as 0x7FFFFFFFFFFFF000.
const cgroupUnlimited = int64(1) << 62

// containerMemoryLimit returns the tightest memory limit set on this process's cgroup or any of
// its ancestors, in bytes, or 0 when none is set.
func containerMemoryLimit() int64 {
	self, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		return 0
	}
	return cgroupMemoryLimit(os.DirFS("/sys/fs/cgroup"), string(self))
}

// cgroupMemoryLimit returns the tightest memory limit along the cgroup paths listed in self, the
// contents of /proc/self/cgroup, reading limit files from cgroupFS, the cgroup mount. It returns 0
// when no limit is set.
func cgroupMemoryLimit(cgroupFS fs.FS, self string) int64 {
	var tightest int64
	for _, line := range strings.Split(strings.TrimSpace(self), "\n") {
		fields := strings.SplitN(line, ":", 3)
		if len(fields) != 3 {
			continue
		}
		var dir, file string
		switch {
		case fields[0] == "0" && fields[1] == "":
			dir, file = fields[2], "memory.max"
		case slices.Contains(strings.Split(fields[1], ","), "memory"):
			dir, file = path.Join("memory", fields[2]), "memory.limit_in_bytes"
		default:
			continue
		}
		for dir = strings.TrimPrefix(path.Clean("/"+dir), "/"); ; dir = path.Dir(dir) {
			if dir == "" {
				dir = "."
			}
			if n := readCgroupLimit(cgroupFS, path.Join(dir, file)); n > 0 && (tightest == 0 || n < tightest) {
				tightest = n
			}
			if dir == "." {
				break
			}
		}
	}
	return tightest
}

// readCgroupLimit returns the limit in name, or 0 when the file is missing, unset, or unlimited.
func readCgroupLimit(cgroupFS fs.FS, name string) int64 {
	raw, err := fs.ReadFile(cgroupFS, name)
	if err != nil {
		return 0
	}
	n, err := strconv.ParseInt(strings.TrimSpace(string(raw)), 10, 64)
	if err != nil || n <= 0 || n >= cgroupUnlimited {
		return 0
	}
	return n
}
