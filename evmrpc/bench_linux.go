//go:build linux

package evmrpc

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

// benchPin sets the calling thread's affinity to one vCPU and returns a
// function that restores the previous mask.
func benchPin(cpu int) (func(), error) {
	var old unix.CPUSet
	if err := unix.SchedGetaffinity(0, &old); err != nil {
		return nil, err
	}
	var set unix.CPUSet
	set.Zero()
	set.Set(cpu)
	if err := unix.SchedSetaffinity(0, &set); err != nil {
		return nil, fmt.Errorf("sched_setaffinity(%d): %w", cpu, err)
	}
	return func() { _ = unix.SchedSetaffinity(0, &old) }, nil
}

func benchSiblingOf(cpu int) int {
	b, err := os.ReadFile(fmt.Sprintf("/sys/devices/system/cpu/cpu%d/topology/thread_siblings_list", cpu))
	if err != nil {
		return -1
	}
	for _, part := range strings.Split(strings.TrimSpace(string(b)), ",") {
		if strings.Contains(part, "-") {
			ab := strings.SplitN(part, "-", 2)
			lo, _ := strconv.Atoi(ab[0])
			hi, _ := strconv.Atoi(ab[1])
			for i := lo; i <= hi; i++ {
				if i != cpu {
					return i
				}
			}
			continue
		}
		if v, err := strconv.Atoi(strings.TrimSpace(part)); err == nil && v != cpu {
			return v
		}
	}
	return -1
}

type benchCPUT struct{ busy, total uint64 }

func benchCPUTimes() []benchCPUT {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []benchCPUT
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fs := strings.Fields(sc.Text())
		if len(fs) < 8 || !strings.HasPrefix(fs[0], "cpu") || fs[0] == "cpu" {
			continue
		}
		idx, err := strconv.Atoi(fs[0][3:])
		if err != nil {
			continue
		}
		var v [10]uint64
		for i := 1; i < len(fs) && i <= 10; i++ {
			v[i-1], _ = strconv.ParseUint(fs[i], 10, 64)
		}
		idle := v[3] + v[4]
		busy := v[0] + v[1] + v[2] + v[5] + v[6] + v[7]
		for len(out) <= idx {
			out = append(out, benchCPUT{})
		}
		out[idx] = benchCPUT{busy: busy, total: busy + idle}
	}
	return out
}

func benchBusyBetween(a, b []benchCPUT) []float64 {
	if len(a) == 0 || len(a) != len(b) {
		return nil
	}
	out := make([]float64, len(a))
	for i := range a {
		dt := b[i].total - a[i].total
		if dt == 0 {
			continue
		}
		out[i] = float64(b[i].busy-a[i].busy) / float64(dt)
	}
	return out
}
