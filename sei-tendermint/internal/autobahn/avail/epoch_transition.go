package avail

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

func logStaleRoad(what string, roadIdx types.RoadIndex, duo types.EpochDuo) {
	// Debug: Info is too chatty at epoch boundaries (many peers a road behind).
	logger.Debug("dropping stale "+what+": road behind window",
		slog.Uint64("road", uint64(roadIdx)), "duo", duo.String())
}

// waitUntilRoad waits until status is not RoadFuture; RoadStale → ErrPruned
// with the deciding duo still returned for logging.
func (s *State) waitUntilRoad(
	ctx context.Context,
	roadIdx types.RoadIndex,
	status func(types.EpochDuo) types.RoadStatus,
) (types.EpochDuo, error) {
	duo, err := s.epochDuo.Wait(ctx, func(duo types.EpochDuo) bool {
		return status(duo) != types.RoadFuture
	})
	if err != nil {
		return types.EpochDuo{}, err
	}
	switch status(duo) {
	case types.RoadReady:
		return duo, nil
	case types.RoadStale:
		return duo, types.ErrPruned
	default:
		// Wait predicate forbids Future; hitting it is an internal bug.
		panic(fmt.Sprintf("waitUntilRoad: unexpected RoadFuture for road %d after Wait", roadIdx))
	}
}

// waitForEpoch waits until roadIdx is in Current (CommitQC tip).
func (s *State) waitForEpoch(ctx context.Context, roadIdx types.RoadIndex) (types.EpochDuo, error) {
	return s.waitUntilRoad(ctx, roadIdx, func(d types.EpochDuo) types.RoadStatus {
		return d.RoadStatusCurrent(roadIdx)
	})
}

// waitForEpochDuo waits until roadIdx is in Prev|Current (AppVote/AppQC).
func (s *State) waitForEpochDuo(ctx context.Context, roadIdx types.RoadIndex) (types.EpochDuo, error) {
	return s.waitUntilRoad(ctx, roadIdx, func(d types.EpochDuo) types.RoadStatus {
		return d.RoadStatusDuo(roadIdx)
	})
}

// waitForEpochOrDropStale is PushCommitQC admit: wait for Current, soft-drop if stale.
func (s *State) waitForEpochOrDropStale(
	ctx context.Context, what string, roadIdx types.RoadIndex,
) (utils.Option[types.EpochDuo], error) {
	return s.waitRoadOrDropStale(ctx, what, roadIdx, s.waitForEpoch)
}

// waitForEpochDuoOrDropStale is PushAppVote/PushAppQC admit: wait for Prev|Current, soft-drop if stale.
func (s *State) waitForEpochDuoOrDropStale(
	ctx context.Context, what string, roadIdx types.RoadIndex,
) (utils.Option[types.EpochDuo], error) {
	return s.waitRoadOrDropStale(ctx, what, roadIdx, s.waitForEpochDuo)
}

func (s *State) waitRoadOrDropStale(
	ctx context.Context,
	what string,
	roadIdx types.RoadIndex,
	wait func(context.Context, types.RoadIndex) (types.EpochDuo, error),
) (utils.Option[types.EpochDuo], error) {
	duo, err := wait(ctx, roadIdx)
	if err != nil {
		if errors.Is(err, types.ErrPruned) {
			logStaleRoad(what, roadIdx, duo)
			return utils.None[types.EpochDuo](), nil
		}
		return utils.None[types.EpochDuo](), err
	}
	return utils.Some(duo), nil
}

// waitForAppQC blocks until latest AppQC is from epochIdx or later.
//
// Seal prune leash (interlocking doc): Availability admits the last CommitQC of
// epoch N only after an AppQC for N exists. Also used by runAdvanceEpoch as a
// no-op once admit already waited. Epoch 0 is not special-cased: leaving 0
// still needs an AppQC anchor for restart (Current>0 requires one).
func (s *State) waitForAppQC(ctx context.Context, epochIdx types.EpochIndex) error {
	for inner, ctrl := range s.inner.Lock() {
		ready := func() bool {
			appQC, ok := inner.app.latestAppQC.Get()
			if !ok {
				return false
			}
			return appQC.Proposal().EpochIndex() >= epochIdx
		}
		if ready() {
			return nil
		}
		attrs := []any{slog.Uint64("want_epoch", uint64(epochIdx))}
		if appQC, ok := inner.app.latestAppQC.Get(); ok {
			attrs = append(attrs,
				slog.Uint64("latest_app_qc_road", uint64(appQC.Proposal().RoadIndex())),
				slog.Uint64("latest_app_qc_epoch", uint64(appQC.Proposal().EpochIndex())),
			)
		}
		logger.Warn("waiting for AppQC before sealing epoch", attrs...)
		return ctrl.WaitUntil(ctx, ready)
	}
	panic("unreachable")
}

// waitSealLeashes enforces the interlocking-doc seal conditions before admitting
// the last CommitQC of ep: AppQC for ep (unless incomingAppEpoch already
// satisfies) and registry WaitForDuo for the next road (execution leash).
func (s *State) waitSealLeashes(
	ctx context.Context,
	ep *types.Epoch,
	idx types.RoadIndex,
	incomingAppEpoch utils.Option[types.EpochIndex],
) error {
	last := ep.RoadRange().Next - 1
	if idx != last {
		return nil
	}
	if e, ok := incomingAppEpoch.Get(); !ok || e < ep.EpochIndex() {
		if err := s.waitForAppQC(ctx, ep.EpochIndex()); err != nil {
			return err
		}
	}
	if _, err := s.data.Registry().WaitForDuo(ctx, last+1); err != nil {
		return fmt.Errorf("WaitForDuo(%d): %w", last+1, err)
	}
	return nil
}

// runAdvanceEpoch is the sole post-construction writer of epochDuo. When
// commitQCs tip passes Current's last road, seal leashes have already been
// satisfied at PushCommitQC/PushAppQC admit; this waits for tip, re-checks
// leashes (no-op if already met), then advances. N+1 CommitQCs park on
// waitForEpoch until the duo slides.
func (s *State) runAdvanceEpoch(ctx context.Context) error {
	for {
		duo := s.epochDuo.Load()
		epochIdx := duo.Current.EpochIndex()
		last := duo.Current.RoadRange().Next - 1

		for inner, ctrl := range s.inner.Lock() {
			if err := ctrl.WaitUntil(ctx, func() bool {
				return inner.commits.qcs.next > last
			}); err != nil {
				return err
			}
		}

		if err := s.waitForAppQC(ctx, epochIdx); err != nil {
			return err
		}
		nextDuo, err := s.data.Registry().WaitForDuo(ctx, last+1)
		if err != nil {
			return err
		}

		for inner, ctrl := range s.inner.Lock() {
			live := inner.epoch.duo.Load()
			if live.Current.EpochIndex() != epochIdx {
				break
			}
			if inner.commits.qcs.next <= last {
				break
			}
			inner.advanceEpoch(nextDuo)
			ctrl.Updated()
		}
	}
}
