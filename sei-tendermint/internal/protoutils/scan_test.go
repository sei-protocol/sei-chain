package protoutils_test

import (
	"testing"
)

func TestScan(t *testing.T) {
	// TODO: for SizedOk, OuterSized, OuterNotSized msg 
	// * populate msg recursively to the limits on all fields with randomized content via TestRng. 
	// * check that Scan passes.
	// * for every field exceed the limit slightly and check that Scan detects it
	// Keep the test compact - single test, not subtests
	// Keep it table-driven as long as it makes the test more compact.
}
