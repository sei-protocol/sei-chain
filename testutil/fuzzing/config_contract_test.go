package fuzzing_test

import (
	"testing"

	"github.com/sei-protocol/sei-chain/testutil/fuzzing"
)

// TestEveryKindHasAName closes the half of the bound the array literal cannot.
//
// ConfigValueKindName builds a [ConfigValueKinds]string, so supplying too many names fails to
// compile. Supplying too few does not: the trailing entries are the zero string, and a kind
// added to ConfigValue without a matching name would report itself as "" in exactly the
// failure message that exists to identify it.
func TestEveryKindHasAName(t *testing.T) {
	for k := 0; k < int(fuzzing.ConfigValueKinds); k++ {
		if name := fuzzing.ConfigValueKindName(uint8(k)); name == "" {
			t.Errorf("kind %d has no name. A kind was added to ConfigValue without adding one, "+
				"so a failure on this kind would say \"kind %d ()\" and identify nothing", k, k)
		}
	}
}
