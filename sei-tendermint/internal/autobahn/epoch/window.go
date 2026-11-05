package epoch

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/hashable"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// Window is the epoch state committed with the first block of an epoch.
type Window struct {
	prev    utils.Option[*types.Epoch]
	current *types.Epoch
	next    *types.Epoch
}

// NewWindow returns a validated window of consecutive epochs. Prev is absent
// when the previous epoch has been pruned or does not exist.
func NewWindow(prev utils.Option[*types.Epoch], current, next *types.Epoch) (Window, error) {
	if prevEpoch, ok := prev.Get(); ok {
		if err := checkConsecutive(prevEpoch, current); err != nil {
			return Window{}, fmt.Errorf("prev-current: %w", err)
		}
		if err := checkDerived(prevEpoch, current); err != nil {
			return Window{}, fmt.Errorf("current: %w", err)
		}
	}
	if err := checkConsecutive(current, next); err != nil {
		return Window{}, fmt.Errorf("current-next: %w", err)
	}
	if err := checkDerived(current, next); err != nil {
		return Window{}, fmt.Errorf("next: %w", err)
	}
	return Window{prev: prev, current: current, next: next}, nil
}

// Prev returns the epoch closed immediately before Current, if retained.
func (w Window) Prev() utils.Option[*types.Epoch] { return w.prev }

// Current returns the epoch that opens with the block committing this window.
func (w Window) Current() *types.Epoch { return w.current }

// Next returns the epoch staged by the block before Current starts.
func (w Window) Next() *types.Epoch { return w.next }

// Equal reports whether w and other contain the same epochs.
func (w Window) Equal(other Window) bool {
	return optionEpochEqual(w.prev, other.prev) &&
		epochEqual(w.current, other.current) &&
		epochEqual(w.next, other.next)
}

func checkConsecutive(a, b *types.Epoch) error {
	if b.EpochIndex() != a.EpochIndex()+1 {
		return fmt.Errorf("epoch %d does not follow %d", b.EpochIndex(), a.EpochIndex())
	}
	if a.RoadRange().Next != b.RoadRange().First {
		return fmt.Errorf("epoch %d road range does not meet epoch %d", a.EpochIndex(), b.EpochIndex())
	}
	return nil
}

func optionEpochEqual(a, b utils.Option[*types.Epoch]) bool {
	av, aok := a.Get()
	bv, bok := b.Get()
	if aok != bok {
		return false
	}
	return !aok || epochEqual(av, bv)
}

func epochEqual(a, b *types.Epoch) bool {
	return a.EpochIndex() == b.EpochIndex() &&
		a.RoadRange() == b.RoadRange() &&
		a.Committee().Equal(b.Committee())
}

// MarshalWindow returns the canonical encoding of w, which chain state stores and hashes.
func MarshalWindow(w Window) []byte {
	return hashable.MarshalCanonical(WindowConv.Encode(w))
}

// WindowConv converts an epoch window to and from protobuf.
var WindowConv = protoutils.Conv[Window, *pb.EpochWindow]{
	Encode: func(w Window) *pb.EpochWindow {
		return &pb.EpochWindow{
			Prev:    types.EpochConv.EncodeOpt(w.prev),
			Current: types.EpochConv.Encode(w.current),
			Next:    types.EpochConv.Encode(w.next),
		}
	},
	Decode: func(record *pb.EpochWindow) (Window, error) {
		if record == nil {
			return Window{}, fmt.Errorf("missing")
		}
		prev, err := types.EpochConv.DecodeOpt(record.Prev)
		if err != nil {
			return Window{}, fmt.Errorf("prev: %w", err)
		}
		current, err := types.EpochConv.DecodeReq(record.Current)
		if err != nil {
			return Window{}, fmt.Errorf("current: %w", err)
		}
		next, err := types.EpochConv.DecodeReq(record.Next)
		if err != nil {
			return Window{}, fmt.Errorf("next: %w", err)
		}
		return NewWindow(prev, current, next)
	},
}
