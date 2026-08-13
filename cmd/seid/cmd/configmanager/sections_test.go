package configmanager

import (
	"strings"
	"testing"

	"github.com/sei-protocol/sei-chain/config/sections"
)

// TestTheBootSeesEverySectionThisBinaryDeclares is what makes the registration import load-bearing.
//
// A section absent here is one whose values are never installed into the booting node, so its keys
// resolve through the machinery that answered them before. Nothing else in this package would notice,
// because an undeclared key is delegated by design.
func TestTheBootSeesEverySectionThisBinaryDeclares(t *testing.T) {
	if missing := sections.Missing(); len(missing) > 0 {
		t.Fatalf("these sections are absent from this test binary: %s\n\nTheir values are not installed "+
			"into a booting node and the delegation that covers them is silent by design",
			strings.Join(missing, ", "))
	}
}
