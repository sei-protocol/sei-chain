package types

import (
	"errors"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// EpochDuo is a Prev|Current epoch window. Current is always set; Prev is
// absent iff Current is epoch 0, otherwise contiguous with Current (index
// Current-1 and Prev.Next == Current.First).
type EpochDuo struct {
	Prev    utils.Option[*Epoch]
	Current *Epoch
}

// NewEpochDuo builds a Prev|Current window. Panics unless
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

var ErrRoadBeforeWindow = errors.New("road before epoch duo window")
var ErrRoadAfterWindow = errors.New("road after epoch duo window")

// RoadStatus classifies a road relative to a Prev|Current window for admit waits.
type RoadStatus int

const (
	RoadReady  RoadStatus = iota // in the admitted window
	RoadStale                    // behind the window (soft-drop / ErrPruned)
	RoadFuture                   // ahead of the window (backpressure wait)
)

// RoadStatusCurrent classifies roadIdx against Current only (CommitQC tip).
func (w EpochDuo) RoadStatusCurrent(roadIdx RoadIndex) RoadStatus {
	if w.Current.RoadRange().Has(roadIdx) {
		return RoadReady
	}
	if roadIdx < w.Current.RoadRange().First {
		return RoadStale
	}
	return RoadFuture
}

// RoadStatusDuo classifies roadIdx against Prev|Current (AppVote/AppQC).
func (w EpochDuo) RoadStatusDuo(roadIdx RoadIndex) RoadStatus {
	_, err := w.EpochForRoad(roadIdx)
	if err == nil {
		return RoadReady
	}
	if errors.Is(err, ErrRoadBeforeWindow) {
		return RoadStale
	}
	return RoadFuture
}

// EpochForRoad returns the epoch containing roadIdx.
// Window is [Prev.First or Current.First, Current.Next). Outside →
// ErrRoadBeforeWindow / ErrRoadAfterWindow. Under contiguous Prev|Current there
// is no gap, so a miss after the after-window check is always before-window.
func (w EpochDuo) EpochForRoad(roadIdx RoadIndex) (*Epoch, error) {
	if roadIdx >= w.Current.RoadRange().Next {
		return nil, fmt.Errorf("road %d after window %v: %w", roadIdx, w, ErrRoadAfterWindow)
	}
	if w.Current.RoadRange().Has(roadIdx) {
		return w.Current, nil
	}
	if prev, ok := w.Prev.Get(); ok && prev.RoadRange().Has(roadIdx) {
		return prev, nil
	}
	return nil, fmt.Errorf("road %d before window %v: %w", roadIdx, w, ErrRoadBeforeWindow)
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
