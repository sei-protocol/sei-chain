package types

import (
	"errors"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// EpochDuo is a Prev|Current epoch pair. It is a value for verifying a tipcut
// that may carry an AppQC from the previous epoch — not a shared cross-layer
// state machine. Live avail tip state is Current + App prune anchor instead.
//
// Current is always set; Prev is absent iff Current is epoch 0, otherwise
// contiguous with Current (index Current-1 and Prev.Next == Current.First).
type EpochDuo struct {
	Prev    utils.Option[*Epoch]
	Current *Epoch
}

// NewEpochDuo builds a Prev|Current pair. Panics unless
// prev≠None ⇔ current.EpochIndex()>0, and when Prev is present it must be
// contiguous with Current.
func NewEpochDuo(current *Epoch, prev utils.Option[*Epoch]) EpochDuo {
	cur := current.EpochIndex()
	p, hasPrev := prev.Get()
	if hasPrev != (cur > 0) {
		panic(fmt.Sprintf("NewEpochDuo: Prev present=%v but Current epoch %d (want Prev iff Current>0)",
			hasPrev, cur))
	}
	if hasPrev {
		if p.EpochIndex()+1 != cur {
			panic(fmt.Sprintf("NewEpochDuo: Prev epoch %d not contiguous with Current %d",
				p.EpochIndex(), cur))
		}
		if got, want := p.RoadRange().Next, current.RoadRange().First; got != want {
			panic(fmt.Sprintf("NewEpochDuo: Prev roads end at %d, Current starts at %d", got, want))
		}
	}
	return EpochDuo{Prev: prev, Current: current}
}

// ByRoad returns the epoch containing roadIdx within this pair.
// Future roads and roads before Prev return an error (ErrPruned when behind).
func (w EpochDuo) ByRoad(roadIdx RoadIndex) (*Epoch, error) {
	if roadIdx >= w.Current.RoadRange().Next {
		return nil, errors.New("road belongs to future epoch")
	}
	if w.Current.RoadRange().Has(roadIdx) {
		return w.Current, nil
	}
	if prev, ok := w.Prev.Get(); ok && prev.RoadRange().Has(roadIdx) {
		return prev, nil
	}
	return nil, ErrPruned
}

func (w EpochDuo) String() string {
	s := "epochs ["
	sep := ""
	// Prev then Current so a window {4,5} prints as "epochs [4, 5]".
	for _, oep := range [2]utils.Option[*Epoch]{w.Prev, utils.Some(w.Current)} {
		if ep, ok := oep.Get(); ok {
			s += fmt.Sprintf("%s%d", sep, ep.EpochIndex())
			sep = ", "
		}
	}
	return s + "]"
}
