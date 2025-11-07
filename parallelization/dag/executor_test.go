package dag_test

import (
	"testing"

	"github.com/sei-protocol/sei-chain/parallelization/dag"
)

func TestRunExample(t *testing.T) {
	if err := dag.RunExample(); err != nil {
		t.Fatalf("RunExample returned error: %v", err)
	}
}
