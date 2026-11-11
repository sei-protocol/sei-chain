package p2p

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	dto "github.com/prometheus/client_model/go"
	"google.golang.org/protobuf/encoding/protowire"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/statesyncproof"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
)

func TestStateSyncProofExporterWritesPersistedCheckpoint(t *testing.T) {
	rng := utils.TestRng()
	env := newStateSyncProofExportEnv(t, rng)

	require.NoError(t, env.exporter.sweepOnce())

	decoded := env.readStateSyncProof(t, env.index)
	require.Equal(t, env.index, decoded.Index())
	require.Equal(t, env.roadBlock.Header().Hash(), decoded.RoadBlock().Header().Hash())
	require.Equal(t, env.appQC.Proposal().Index(), decoded.AppQC().Proposal().Index())
}

func TestStateSyncProofExporterWaitsForDurableAppQC(t *testing.T) {
	rng := utils.TestRng()
	env := newStateSyncProofExportEnv(t, rng)
	env.anchorSend.Store(data.TestAnchor(env.epoch))

	require.NoError(t, env.exporter.sweepOnce())
	require.NoFileExists(t, env.stateSyncProofPath(env.index))

	env.anchorSend.Store(env.anchorAt(env.anchor))
	require.NoError(t, env.exporter.sweepOnce())
	require.FileExists(t, env.stateSyncProofPath(env.index))
}

func TestStateSyncProofExporterWaitsForDurableAppQCAtCheckpoint(t *testing.T) {
	rng := utils.TestRng()
	env := newStateSyncProofExportEnv(t, rng)
	next := env.addNextRoad(rng)
	nextIndex := next.AppQC.Proposal().Index()

	require.NoError(t, env.exporter.sweepOnce())
	require.NoFileExists(t, env.stateSyncProofPath(nextIndex))

	env.anchorSend.Store(env.anchorAt(next))
	require.NoError(t, env.exporter.sweepOnce())
	require.FileExists(t, env.stateSyncProofPath(nextIndex))
}

func TestStateSyncProofExporterCatchesCheckpointAfterAppQC(t *testing.T) {
	rng := utils.TestRng()
	env := newStateSyncProofExportEnv(t, rng)
	env.checkpoints = nil

	require.NoError(t, env.exporter.sweepOnce())
	require.NoFileExists(t, env.stateSyncProofPath(env.index))

	env.checkpoints = []int64{int64(env.index)}
	require.NoError(t, env.exporter.sweepOnce())
	require.FileExists(t, env.stateSyncProofPath(env.index))
}

func TestStateSyncProofExporterSkipsLastRoad(t *testing.T) {
	rng := utils.TestRng()
	env := newStateSyncProofExportEnv(t, rng)
	env.exporter.closingEpoch = func(types.RoadIndex) utils.Option[types.EpochIndex] {
		return utils.Some(types.EpochIndex(1))
	}

	require.NoError(t, env.exporter.sweepOnce())
	require.NoFileExists(t, env.stateSyncProofPath(env.index))
}

func TestStateSyncProofExporterSkipsMissingOrPrunedInputs(t *testing.T) {
	cases := map[string]func(*stateSyncProofExportEnv){
		"pruned block": func(env *stateSyncProofExportEnv) {
			env.store.readBlockErr = types.ErrPruned
		},
		"missing block": func(env *stateSyncProofExportEnv) {
			delete(env.store.blocks, env.index)
		},
		"pruned AppProposal": func(env *stateSyncProofExportEnv) {
			env.store.readProposalErr = types.ErrPruned
		},
		"missing AppProposal": func(env *stateSyncProofExportEnv) {
			delete(env.store.appProposals, env.index)
		},
		"pruned AppQC": func(env *stateSyncProofExportEnv) {
			env.store.readAppQCErr = types.ErrPruned
		},
		"missing AppQC": func(env *stateSyncProofExportEnv) {
			delete(env.store.appQCs, env.index)
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			rng := utils.TestRng()
			env := newStateSyncProofExportEnv(t, rng)
			mutate(env)

			require.NoError(t, env.exporter.sweepOnce())
			require.NoFileExists(t, env.stateSyncProofPath(env.index))
		})
	}
}

func TestStateSyncProofExporterReturnsReadErrors(t *testing.T) {
	cases := map[string]func(*stateSyncProofExportEnv){
		"block": func(env *stateSyncProofExportEnv) {
			env.store.readBlockErr = errors.New("block read failed")
		},
		"AppProposal": func(env *stateSyncProofExportEnv) {
			env.store.readProposalErr = errors.New("proposal read failed")
		},
		"AppQC": func(env *stateSyncProofExportEnv) {
			env.store.readAppQCErr = errors.New("app qc read failed")
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			rng := utils.TestRng()
			env := newStateSyncProofExportEnv(t, rng)
			mutate(env)

			require.Error(t, env.exporter.sweepOnce())
			require.NoFileExists(t, env.stateSyncProofPath(env.index))
		})
	}
}

func TestStateSyncProofExporterSkipsInconsistentRecords(t *testing.T) {
	cases := map[string]func(*stateSyncProofExportEnv, utils.Rng){
		"AppHash mismatch": func(env *stateSyncProofExportEnv, _ utils.Rng) {
			env.store.appProposals[env.index] = types.NewAppProposal(env.roadBlock.QC().QC().View(), []byte{9, 9, 9})
		},
		"AppQC of another road": func(env *stateSyncProofExportEnv, rng utils.Rng) {
			next := env.addNextRoad(rng)
			nextIndex := next.AppQC.Proposal().Index()
			env.store.appProposals[env.index] = env.store.appProposals[nextIndex]
			env.store.appQCs[env.index] = next.AppQC
			env.checkpoints = []int64{int64(env.index)}
		},
		"road block of another road": func(env *stateSyncProofExportEnv, rng utils.Rng) {
			next := env.addNextRoad(rng)
			env.store.blocks[env.index] = env.store.blocks[next.AppQC.Proposal().Index()]
			env.checkpoints = []int64{int64(env.index)}
		},
	}
	for name, corrupt := range cases {
		t.Run(name, func(t *testing.T) {
			rng := utils.TestRng()
			env := newStateSyncProofExportEnv(t, rng)
			corrupt(env, rng)
			before := stateSyncProofExportFailures(t)

			require.NoError(t, env.exporter.sweepOnce())
			require.NoFileExists(t, env.stateSyncProofPath(env.index))
			require.Equal(t, before+1, stateSyncProofExportFailures(t))
		})
	}
}

func TestStateSyncProofExporterSweepContinuesOnIOError(t *testing.T) {
	rng := utils.TestRng()
	env := newStateSyncProofExportEnv(t, rng)
	env.store.readBlockErr = errors.New("disk failed")
	before := stateSyncProofExportFailures(t)

	env.exporter.sweepAndHandleError()
	env.exporter.sweepAndHandleError()
	require.Equal(t, before+2, stateSyncProofExportFailures(t))
	require.NotEqual(t, "", env.exporter.lastErr)

	env.store.readBlockErr = nil
	env.exporter.sweepAndHandleError()
	require.Equal(t, "", env.exporter.lastErr)
	require.FileExists(t, env.stateSyncProofPath(env.index))
}

func TestRunStateSyncProofExportSweepsOnAnchorUpdate(t *testing.T) {
	rng := utils.TestRng()
	env := newStateSyncProofExportEnv(t, rng)
	store := &failFirstReadBlockStore{
		fakeStateSyncProofBlockStore: env.store,
		reads:                        utils.NewAtomicSend(0),
	}
	cfg := env.exporter.cfg
	cfg.BlockStore = store

	err := scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		s.SpawnBg(func() error {
			return utils.IgnoreCancel(runStateSyncProofExport(ctx, cfg, env.exporter.closingEpoch))
		})
		reads := store.reads.Subscribe()
		// The startup sweep fails to read the block; the loop must keep running.
		if _, err := reads.Wait(ctx, func(n int) bool { return n >= 1 }); err != nil {
			return err
		}
		env.anchorSend.Store(env.anchorAt(env.anchor))
		_, err := reads.Wait(ctx, func(n int) bool { return n >= 2 })
		return err
	})

	require.NoError(t, err)
	require.FileExists(t, env.stateSyncProofPath(env.index))
}

func TestStateSyncProofExporterKeepsValidExistingStateSyncProof(t *testing.T) {
	rng := utils.TestRng()
	env := newStateSyncProofExportEnv(t, rng)
	old, err := statesyncproof.Encode(env.roadBlock, env.appQC)
	require.NoError(t, err)
	// An unknown field keeps the file valid and makes its bytes differ from a fresh export.
	old = protowire.AppendTag(old, 99, protowire.BytesType)
	old = protowire.AppendBytes(old, []byte("old"))
	env.writeStateSyncProof(t, env.index, old)

	require.NoError(t, env.exporter.sweepOnce())

	got, err := os.ReadFile(env.stateSyncProofPath(env.index))
	require.NoError(t, err)
	require.True(t, bytes.Equal(old, got))
}

func TestStateSyncProofExporterReplacesInvalidStateSyncProofAtStartup(t *testing.T) {
	cases := map[string]func(*stateSyncProofExportEnv, utils.Rng) []byte{
		"garbage": func(*stateSyncProofExportEnv, utils.Rng) []byte {
			return []byte("garbage")
		},
		"wrong road": func(env *stateSyncProofExportEnv, rng utils.Rng) []byte {
			next := env.addNextRoad(rng)
			roadBlock := env.store.blocks[next.AppQC.Proposal().Index()]
			encoded, err := statesyncproof.Encode(roadBlock, next.AppQC)
			require.NoError(t, err)
			env.checkpoints = []int64{int64(env.index)}
			return encoded
		},
	}
	for name, invalid := range cases {
		t.Run(name, func(t *testing.T) {
			rng := utils.TestRng()
			env := newStateSyncProofExportEnv(t, rng)
			env.writeStateSyncProof(t, env.index, invalid(env, rng))

			require.NoError(t, env.exporter.sweepOnce())

			decoded := env.readStateSyncProof(t, env.index)
			require.Equal(t, env.roadBlock.Header().Hash(), decoded.RoadBlock().Header().Hash())
		})
	}
}

func TestStateSyncProofExporterRemovesInvalidStateSyncProofOfPrunedCheckpoint(t *testing.T) {
	rng := utils.TestRng()
	env := newStateSyncProofExportEnv(t, rng)
	env.store.readBlockErr = types.ErrPruned
	env.writeStateSyncProof(t, env.index, []byte("garbage"))

	require.NoError(t, env.exporter.sweepOnce())
	require.NoFileExists(t, env.stateSyncProofPath(env.index))
}

func TestStateSyncProofExporterRewritesDeletedStateSyncProofOnNextCheckpoint(t *testing.T) {
	rng := utils.TestRng()
	env := newStateSyncProofExportEnv(t, rng)
	require.NoError(t, env.exporter.sweepOnce())
	require.NoError(t, os.Remove(env.stateSyncProofPath(env.index)))

	env.checkpoints = append(env.checkpoints, int64(env.index)+1)
	require.NoError(t, env.exporter.sweepOnce())
	require.FileExists(t, env.stateSyncProofPath(env.index))
}

func TestStateSyncProofExporterRemovesOrphansAndTempFiles(t *testing.T) {
	rng := utils.TestRng()
	env := newStateSyncProofExportEnv(t, rng)
	orphan := env.stateSyncProofPath(env.index + 1)
	tmp := env.stateSyncProofPath(env.index) + ".tmp"
	require.NoError(t, os.MkdirAll(env.dir, 0o750))
	require.NoError(t, os.WriteFile(orphan, []byte("orphan"), 0o600))
	require.NoError(t, os.WriteFile(tmp, []byte("tmp"), 0o600))

	require.NoError(t, env.exporter.sweepOnce())

	require.NoFileExists(t, orphan)
	require.NoFileExists(t, tmp)
	require.FileExists(t, env.stateSyncProofPath(env.index))
}

func TestStateSyncProofExporterSkipsSettledCheckpoints(t *testing.T) {
	cases := map[string]func(*stateSyncProofExportEnv){
		"exported": func(*stateSyncProofExportEnv) {},
		"pruned": func(env *stateSyncProofExportEnv) {
			env.store.readBlockErr = types.ErrPruned
		},
		"missing": func(env *stateSyncProofExportEnv) {
			delete(env.store.blocks, env.index)
		},
		"inconsistent": func(env *stateSyncProofExportEnv) {
			env.store.appProposals[env.index] = types.NewAppProposal(env.roadBlock.QC().QC().View(), []byte{9, 9, 9})
		},
		"last road": func(env *stateSyncProofExportEnv) {
			env.exporter.closingEpoch = func(types.RoadIndex) utils.Option[types.EpochIndex] {
				return utils.Some(types.EpochIndex(1))
			}
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			rng := utils.TestRng()
			env := newStateSyncProofExportEnv(t, rng)
			mutate(env)
			closingCalls := 0
			closingEpoch := env.exporter.closingEpoch
			env.exporter.closingEpoch = func(index types.RoadIndex) utils.Option[types.EpochIndex] {
				closingCalls++
				return closingEpoch(index)
			}

			require.NoError(t, env.exporter.sweepOnce())
			blockReads, closingChecks := env.store.blockReads, closingCalls
			require.NoError(t, env.exporter.sweepOnce())
			require.Equal(t, blockReads, env.store.blockReads)
			require.Equal(t, closingChecks, closingCalls)
		})
	}
}

func TestStateSyncProofExporterReadErrorDoesNotBlockOtherCheckpoints(t *testing.T) {
	rng := utils.TestRng()
	env := newStateSyncProofExportEnv(t, rng)
	next := env.addNextRoad(rng)
	nextIndex := next.AppQC.Proposal().Index()
	env.checkpoints = []int64{int64(env.index), int64(nextIndex)}
	env.anchorSend.Store(env.anchorAt(next))
	env.store.readBlockErrAt = map[types.RoadIndex]error{env.index: errors.New("disk failed")}

	require.Error(t, env.exporter.sweepOnce())
	require.NoFileExists(t, env.stateSyncProofPath(env.index))
	require.FileExists(t, env.stateSyncProofPath(nextIndex))

	env.store.readBlockErrAt = nil
	require.NoError(t, env.exporter.sweepOnce())
	require.FileExists(t, env.stateSyncProofPath(env.index))
}

func TestStateSyncProofExporterStuckFileDoesNotBlockOtherCheckpoints(t *testing.T) {
	cases := map[string]func(env *stateSyncProofExportEnv, nextIndex types.RoadIndex) types.RoadIndex{
		"unreadable proof file": func(env *stateSyncProofExportEnv, _ types.RoadIndex) types.RoadIndex {
			return env.index
		},
		"unremovable orphan": func(_ *stateSyncProofExportEnv, nextIndex types.RoadIndex) types.RoadIndex {
			return nextIndex + 1
		},
	}
	for name, stuckIndex := range cases {
		t.Run(name, func(t *testing.T) {
			rng := utils.TestRng()
			env := newStateSyncProofExportEnv(t, rng)
			next := env.addNextRoad(rng)
			nextIndex := next.AppQC.Proposal().Index()
			env.checkpoints = []int64{int64(env.index), int64(nextIndex)}
			env.anchorSend.Store(env.anchorAt(next))
			// A non-empty directory under a proof file name can be neither read nor removed.
			stuck := env.stateSyncProofPath(stuckIndex(env, nextIndex))
			require.NoError(t, os.MkdirAll(filepath.Join(stuck, "child"), 0o750))

			require.Error(t, env.exporter.sweepOnce())
			require.FileExists(t, env.stateSyncProofPath(nextIndex))
		})
	}
}

func TestStateSyncProofExporterForgetsDroppedCheckpoints(t *testing.T) {
	rng := utils.TestRng()
	env := newStateSyncProofExportEnv(t, rng)

	require.NoError(t, env.exporter.sweepOnce())
	require.FileExists(t, env.stateSyncProofPath(env.index))

	env.checkpoints = nil
	require.NoError(t, env.exporter.sweepOnce())
	require.NoFileExists(t, env.stateSyncProofPath(env.index))

	env.checkpoints = []int64{int64(env.index)}
	require.NoError(t, env.exporter.sweepOnce())
	require.FileExists(t, env.stateSyncProofPath(env.index))
}

func TestWriteStateSyncProofFileRemovesTempFileWhenRenameFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "target")
	require.NoError(t, os.Mkdir(path, 0o750))

	require.Error(t, writeStateSyncProofFile(path, []byte("data")))

	require.NoFileExists(t, path+".tmp")
}

type stateSyncProofExportEnv struct {
	dir         string
	keys        []types.SecretKey
	epoch       *types.Epoch
	index       types.RoadIndex
	roadBlock   *types.RoadBlock
	appQC       *types.AppQC
	store       *fakeStateSyncProofBlockStore
	anchor      data.AnchorFirst
	anchorSend  *utils.AtomicSend[data.Anchor]
	checkpoints []int64
	exporter    *stateSyncProofExporter
}

func newStateSyncProofExportEnv(t *testing.T, rng utils.Rng) *stateSyncProofExportEnv {
	t.Helper()
	committee, keys := types.GenCommittee(rng, 4)
	epoch := types.GenEpochWithCommittee(rng, committee)
	qc, blocks := data.TestCommitQC(rng, epoch, keys, utils.None[*types.CommitQC]())
	roadBlock := types.NewRoadBlock(qc, blocks)
	index := roadBlock.QC().Index()
	proposal := types.NewAppProposal(roadBlock.QC().QC().View(), []byte{1, 2, 3})
	appQC := data.TestAppQC(keys, proposal)
	anchor := data.AnchorFirst{CommitQC: roadBlock.QC().QC(), AppQC: appQC}
	anchorSend := utils.NewAtomicSend(data.TestAnchor(epoch))
	store := &fakeStateSyncProofBlockStore{
		blocks:       map[types.RoadIndex]*types.RoadBlock{index: roadBlock},
		appProposals: map[types.RoadIndex]*types.AppProposal{index: proposal},
		appQCs:       map[types.RoadIndex]*types.AppQC{index: appQC},
	}
	env := &stateSyncProofExportEnv{
		dir:         t.TempDir(),
		keys:        keys,
		epoch:       epoch,
		index:       index,
		roadBlock:   roadBlock,
		appQC:       appQC,
		anchor:      anchor,
		anchorSend:  &anchorSend,
		store:       store,
		checkpoints: []int64{int64(index)},
	}
	anchorSend.Store(env.anchorAt(anchor))
	env.exporter = newStateSyncProofExporter(
		StateSyncProofExportConfig{
			Dir: env.dir,
			CheckpointHeights: func() ([]int64, error) {
				return slices.Clone(env.checkpoints), nil
			},
			BlockStore: store,
			Anchor:     anchorSend.Subscribe(),
		},
		func(types.RoadIndex) utils.Option[types.EpochIndex] {
			return utils.None[types.EpochIndex]()
		},
	)
	return env
}

// addNextRoad stores the road after env.index with its AppProposal and AppQC, makes it the only
// retained checkpoint, and returns its AnchorFirst. The published Anchor does not change.
func (e *stateSyncProofExportEnv) addNextRoad(rng utils.Rng) data.AnchorFirst {
	qc, blocks := data.TestCommitQC(rng, e.epoch, e.keys, utils.Some(e.roadBlock.QC().QC()))
	roadBlock := types.NewRoadBlock(qc, blocks)
	index := roadBlock.QC().Index()
	proposal := types.NewAppProposal(roadBlock.QC().QC().View(), []byte{4, 5, 6})
	appQC := data.TestAppQC(e.keys, proposal)
	e.store.blocks[index] = roadBlock
	e.store.appProposals[index] = proposal
	e.store.appQCs[index] = appQC
	e.checkpoints = []int64{int64(index)}
	return data.AnchorFirst{CommitQC: roadBlock.QC().QC(), AppQC: appQC}
}

// anchorAt returns an Anchor in env.epoch whose durable AppQC is first.AppQC.
func (e *stateSyncProofExportEnv) anchorAt(first data.AnchorFirst) data.Anchor {
	anchor := data.TestAnchor(e.epoch)
	anchor.First = utils.Some(first)
	return anchor
}

func stateSyncProofExportFailures(t *testing.T) float64 {
	t.Helper()
	var m dto.Metric
	require.NoError(t, Global.gigaStateSyncProofExportFailuresAt().Write(&m))
	return m.GetCounter().GetValue()
}

func (e *stateSyncProofExportEnv) stateSyncProofPath(index types.RoadIndex) string {
	return filepath.Join(e.dir, statesyncproof.FileName(index))
}

func (e *stateSyncProofExportEnv) writeStateSyncProof(t *testing.T, index types.RoadIndex, data []byte) {
	t.Helper()
	require.NoError(t, os.MkdirAll(e.dir, 0o750))
	require.NoError(t, os.WriteFile(e.stateSyncProofPath(index), data, 0o600))
}

func (e *stateSyncProofExportEnv) readStateSyncProof(t *testing.T, index types.RoadIndex) *statesyncproof.Proof {
	t.Helper()
	encoded, err := os.ReadFile(e.stateSyncProofPath(index))
	require.NoError(t, err)
	decoded, err := statesyncproof.Decode(encoded, index)
	require.NoError(t, err)
	return decoded
}

type fakeStateSyncProofBlockStore struct {
	blocks          map[types.RoadIndex]*types.RoadBlock
	appProposals    map[types.RoadIndex]*types.AppProposal
	appQCs          map[types.RoadIndex]*types.AppQC
	readBlockErr    error
	readBlockErrAt  map[types.RoadIndex]error
	readProposalErr error
	readAppQCErr    error
	blockReads      int
}

func (s *fakeStateSyncProofBlockStore) ReadBlock(index types.RoadIndex) (utils.Option[*types.RoadBlock], error) {
	s.blockReads++
	if s.readBlockErr != nil {
		return utils.None[*types.RoadBlock](), s.readBlockErr
	}
	if err, ok := s.readBlockErrAt[index]; ok {
		return utils.None[*types.RoadBlock](), err
	}
	block, ok := s.blocks[index]
	if !ok {
		return utils.None[*types.RoadBlock](), nil
	}
	return utils.Some(block), nil
}

func (s *fakeStateSyncProofBlockStore) ReadAppProposal(index types.RoadIndex) (utils.Option[*types.AppProposal], error) {
	if s.readProposalErr != nil {
		return utils.None[*types.AppProposal](), s.readProposalErr
	}
	proposal, ok := s.appProposals[index]
	if !ok {
		return utils.None[*types.AppProposal](), nil
	}
	return utils.Some(proposal), nil
}

func (s *fakeStateSyncProofBlockStore) ReadAppQC(index types.RoadIndex) (utils.Option[*types.AppQC], error) {
	if s.readAppQCErr != nil {
		return utils.None[*types.AppQC](), s.readAppQCErr
	}
	appQC, ok := s.appQCs[index]
	if !ok {
		return utils.None[*types.AppQC](), nil
	}
	return utils.Some(appQC), nil
}

// failFirstReadBlockStore fails the first ReadBlock call and publishes the number of ReadBlock calls.
type failFirstReadBlockStore struct {
	*fakeStateSyncProofBlockStore
	reads utils.AtomicSend[int]
}

func (s *failFirstReadBlockStore) ReadBlock(index types.RoadIndex) (utils.Option[*types.RoadBlock], error) {
	n := s.reads.Load() + 1
	s.reads.Store(n)
	if n == 1 {
		return utils.None[*types.RoadBlock](), errors.New("disk failed")
	}
	return s.fakeStateSyncProofBlockStore.ReadBlock(index)
}

var (
	_ stateSyncProofBlockStore = (*fakeStateSyncProofBlockStore)(nil)
	_ stateSyncProofBlockStore = (*failFirstReadBlockStore)(nil)
)
