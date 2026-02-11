package protoutils

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"google.golang.org/protobuf/proto"
)

// Test tests whether reencoding a value is an identity operation.
func (c *Conv[T, P]) Test(want T) error {
	p := c.Encode(want)
	raw, err := proto.Marshal(p)
	if err != nil {
		return fmt.Errorf("Marshal(): %w", err)
	}
	if err := proto.Unmarshal(raw, p); err != nil {
		return fmt.Errorf("Unmarshal(): %w", err)
	}
	got, err := c.Decode(p)
	if err != nil {
		return fmt.Errorf("Decode(Encode()): %w", err)
	}
	return utils.TestDiff(want, got)
}
