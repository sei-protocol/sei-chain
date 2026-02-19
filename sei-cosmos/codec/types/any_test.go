package types_test

import (
	"fmt"
	"testing"

	"github.com/gogo/protobuf/proto"

	"github.com/sei-protocol/sei-chain/sei-cosmos/codec/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/testutil/testdata"
)

type errOnMarshal struct {
	testdata.Dog
}

var _ proto.Message = (*errOnMarshal)(nil)

var errAlways = fmt.Errorf("always erroring")

func (eom *errOnMarshal) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return nil, errAlways
}

var eom = &errOnMarshal{}

// Ensure that returning an error doesn't suddenly allocate and waste bytes.
// See https://github.com/cosmos/cosmos-sdk/issues/8537
func TestNewAnyWithCustomTypeURLWithErrorNoAllocation(t *testing.T) {
	allocs := testing.AllocsPerRun(100, func() {
		any, err := types.NewAnyWithValue(eom)
		if err == nil {
			t.Fatal("err wasn't returned")
		}
		if any != nil {
			t.Fatalf("Unexpectedly got a non-nil Any value: %v", any)
		}
	})
	if allocs > 0 {
		t.Errorf("Unexpected allocations: %v per run", allocs)
	}
}

var sink interface{}

func BenchmarkNewAnyWithCustomTypeURLWithErrorReturned(b *testing.B) {
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		any, err := types.NewAnyWithValue(eom)
		if err == nil {
			b.Fatal("err wasn't returned")
		}
		if any != nil {
			b.Fatalf("Unexpectedly got a non-nil Any value: %v", any)
		}
		sink = any
	}
	if sink == nil {
		b.Fatal("benchmark didn't run")
	}
	sink = (interface{})(nil)
}
