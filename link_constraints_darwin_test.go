package sei_test

import "testing"

func TestLinkConstraints(t *testing.T) {
	for _, dir := range linkDirs {
		t.Run(dir, func(t *testing.T) {
			assertSelects(t, dir, nil, "link_mac.go", false)
			assertSelects(t, dir, []string{"muslc"}, "link_mac.go", false)
		})
	}
}
