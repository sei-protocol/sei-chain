package types

import (
	"errors"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// EpochDuo is a sliding window of up to two consecutive epochs.
// Current is always set; Prev is absent only for epoch 0.
// Construct via NewEpochDuo — a zero EpochDuo (nil Current) is invalid.
//
// Next is intentionally not held: new-committee lane traffic is admitted only
// after CommitQC advances Current into the next epoch.
type EpochDuo struct {
	Prev    utils.Option[*Epoch] // absent if Current is epoch 0
	Current *Epoch
}

// NewEpochDuo builds a Prev|Current window. current must be non-nil.
// When prev is present it must be the immediately preceding epoch: index
// Current-1 and road ranges abut (Prev.Next == Current.First).
func NewEpochDuo(current *Epoch, prev utils.Option[*Epoch]) EpochDuo {
	if current == nil {
		panic("NewEpochDuo: Current must be non-nil")
	}
	if p, ok := prev.Get(); ok {
		if p == nil {
			panic("NewEpochDuo: Prev must be non-nil when present")
		}
		if want := current.EpochIndex(); p.EpochIndex()+1 != want {
			panic(fmt.Sprintf("NewEpochDuo: Prev epoch %d not contiguous with Current %d",
				p.EpochIndex(), want))
		}
		if got, want := p.RoadRange().Next, current.RoadRange().First; got != want {
			panic(fmt.Sprintf("NewEpochDuo: Prev roads end at %d, Current starts at %d", got, want))
		}
	}
	return EpochDuo{Prev: prev, Current: current}
}

// ErrRoadBeforeWindow is returned by EpochForRoad when the road is older than
// WindowFirst (behind the duo).
var ErrRoadBeforeWindow = errors.New("road before epoch duo window")

// ErrRoadAfterWindow is returned by EpochForRoad when the road is newer than
// Current (at or past Current.Next).
var ErrRoadAfterWindow = errors.New("road after epoch duo window")

func (w EpochDuo) all() [2]utils.Option[*Epoch] {
	return [2]utils.Option[*Epoch]{utils.Some(w.Current), w.Prev}
}

// EpochForRoad returns the epoch whose road range contains roadIdx.
// The window ends at Current.Next — roads at or past that bound are
// ErrRoadAfterWindow even if Prev uses an open range. Within the window,
// Current is checked before Prev so an open-range Prev cannot mask Current.
// Returns ErrRoadBeforeWindow when older than WindowFirst.
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
	if roadIdx < w.WindowFirst() {
		return nil, fmt.Errorf("road %d before window %v: %w", roadIdx, w, ErrRoadBeforeWindow)
	}
	return nil, fmt.Errorf("road %d after window %v: %w", roadIdx, w, ErrRoadAfterWindow)
}

// EpochOptForRoad is EpochForRoad as an Option (None when out of window).
func (w EpochDuo) EpochOptForRoad(roadIdx RoadIndex) utils.Option[*Epoch] {
	if ep, err := w.EpochForRoad(roadIdx); err == nil {
		return utils.Some(ep)
	}
	return utils.None[*Epoch]()
}

// CurrentForRoad returns Current when roadIdx is in Current's range; else None.
// Unlike EpochOptForRoad, Prev is never admitted.
func (w EpochDuo) CurrentForRoad(roadIdx RoadIndex) utils.Option[*Epoch] {
	if w.Current.RoadRange().Has(roadIdx) {
		return utils.Some(w.Current)
	}
	return utils.None[*Epoch]()
}

// WindowFirst is the earliest road still in Prev|Current.
func (w EpochDuo) WindowFirst() RoadIndex {
	if prev, ok := w.Prev.Get(); ok {
		return prev.RoadRange().First
	}
	return w.Current.RoadRange().First
}

// EpochForIndex returns Current or Prev by epoch index.
func (w EpochDuo) EpochForIndex(idx EpochIndex) (*Epoch, error) {
	if w.Current.EpochIndex() == idx {
		return w.Current, nil
	}
	if prev, ok := w.Prev.Get(); ok && prev.EpochIndex() == idx {
		return prev, nil
	}
	return nil, fmt.Errorf("epoch %d not in window %v", idx, w)
}

// String returns a compact description of the epoch indices in the window.
func (w EpochDuo) String() string {
	s := "epochs ["
	sep := ""
	for _, oep := range w.all() {
		if ep, ok := oep.Get(); ok {
			s += fmt.Sprintf("%s%d", sep, ep.EpochIndex())
			sep = ", "
		}
	}
	return s + "]"
}
