package sei_test

import "testing"

func TestLinkConstraints(t *testing.T) {
	for _, dir := range linkDirs {
		t.Run(dir, func(t *testing.T) {
			assertSelects(t, dir, []string{"muslc"}, "link_muslc_aarch64.go", true)
			assertSelects(t, dir, nil, "link_glibclinux_aarch64.go", true)
			assertSelects(t, dir, []string{"muslc", "sys_wasmvm"}, "link_system.go", false)
		})
	}
}
