package cmd_test

import (
	"strings"
	"testing"

	"github.com/sei-protocol/sei-chain/config/sections"
)

// TestThisBinarySeesEverySectionItDeclares is what makes the registration import load-bearing.
//
// A section reaches the registry through its owning package's initialisation, so the set follows the
// import graph. Two sections were arriving only because this package imports their owner for an
// unrelated reason; dropping that use would have removed their keys from every diagnostic and from
// what a booting node installs, and nothing would have failed.
func TestThisBinarySeesEverySectionItDeclares(t *testing.T) {
	if missing := sections.Missing(); len(missing) > 0 {
		t.Fatalf("these declared sections are absent from this binary: %s\n\nTheir keys resolve through "+
			"the machinery that answered them before the registry existed, which reports nothing",
			strings.Join(missing, ", "))
	}
	if unexpected := sections.Unexpected(); len(unexpected) > 0 {
		t.Fatalf("these sections are registered and not named in config/sections: %s\n\nNothing would "+
			"notice one of them leaving again", strings.Join(unexpected, ", "))
	}
}
