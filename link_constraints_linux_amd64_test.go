package sei_test

import "testing"

func TestLinkConstraints(t *testing.T) {
	for _, dir := range linkDirs {
		t.Run(dir, func(t *testing.T) {
			assertSelects(t, dir, []string{"muslc"}, "link_muslc.go", false)
			assertSelects(t, dir, nil, "link_glibclinux_x86_64.go", false)
			assertSelects(t, dir, []string{"muslc", "sys_wasmvm"}, "link_system.go", false)
		})
	}
}
