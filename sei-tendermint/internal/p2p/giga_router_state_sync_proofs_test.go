package p2p

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	dto "github.com/prometheus/client_model/go"
	"google.golang.org/protobuf/encoding/protowire"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/blockstore"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/statesyncproof"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
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
	env.anchorSend.Store(utils.None[data.Anchor]())

	require.NoError(t, env.exporter.sweepOnce())
	require.NoFileExists(t, env.stateSyncProofPath(env.index))

	env.anchorSend.Store(utils.Some(env.anchor))
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

	env.anchorSend.Store(utils.Some(next))
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

func TestStateSyncProofExporterRefusesAppHashMismatch(t *testing.T) {
	rng := utils.TestRng()
	env := newStateSyncProofExportEnv(t, rng)
	env.store.appProposals[env.index] = types.NewAppProposal(env.roadBlock.QC().QC().View(), []byte{9, 9, 9})

	var invariant stateSyncProofInvariantError
	require.True(t, errors.As(env.exporter.sweepOnce(), &invariant))
	require.NoFileExists(t, env.stateSyncProofPath(env.index))
}

func TestStateSyncProofExporterSweepContinuesOnIOError(t *testing.T) {
	rng := utils.TestRng()
	env := newStateSyncProofExportEnv(t, rng)
	env.store.readBlockErr = errors.New("disk failed")
	before := stateSyncProofExportFailures(t)

	require.NoError(t, env.exporter.sweepAndHandleError())
	require.NoError(t, env.exporter.sweepAndHandleError())
	require.Equal(t, before+2, stateSyncProofExportFailures(t))
	require.NotEqual(t, "", env.exporter.lastErr)

	env.store.readBlockErr = nil
	require.NoError(t, env.exporter.sweepAndHandleError())
	require.Equal(t, "", env.exporter.lastErr)
	require.FileExists(t, env.stateSyncProofPath(env.index))
}

func TestStateSyncProofExporterSweepStopsOnInvariantError(t *testing.T) {
	rng := utils.TestRng()
	env := newStateSyncProofExportEnv(t, rng)
	env.store.appProposals[env.index] = types.NewAppProposal(env.roadBlock.QC().QC().View(), []byte{9, 9, 9})
	before := stateSyncProofExportFailures(t)

	var invariant stateSyncProofInvariantError
	require.True(t, errors.As(env.exporter.sweepAndHandleError(), &invariant))
	require.Equal(t, before, stateSyncProofExportFailures(t))
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

func TestStateSyncProofExporterRetriesMissingRecords(t *testing.T) {
	rng := utils.TestRng()
	env := newStateSyncProofExportEnv(t, rng)
	delete(env.store.blocks, env.index)

	require.NoError(t, env.exporter.sweepOnce())
	require.NoFileExists(t, env.stateSyncProofPath(env.index))

	env.store.blocks[env.index] = env.roadBlock
	require.NoError(t, env.exporter.sweepOnce())
	require.FileExists(t, env.stateSyncProofPath(env.index))
}

func TestStateSyncProofExporterReadErrorDoesNotBlockOtherCheckpoints(t *testing.T) {
	rng := utils.TestRng()
	env := newStateSyncProofExportEnv(t, rng)
	next := env.addNextRoad(rng)
	nextIndex := next.AppQC.Proposal().Index()
	env.checkpoints = []int64{int64(env.index), int64(nextIndex)}
	env.anchorSend.Store(utils.Some(next))
	env.store.readBlockErrAt = map[types.RoadIndex]error{env.index: errors.New("disk failed")}

	require.Error(t, env.exporter.sweepOnce())
	require.NoFileExists(t, env.stateSyncProofPath(env.index))
	require.FileExists(t, env.stateSyncProofPath(nextIndex))

	env.store.readBlockErrAt = nil
	require.NoError(t, env.exporter.sweepOnce())
	require.FileExists(t, env.stateSyncProofPath(env.index))
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
	anchor      data.Anchor
	anchorSend  *utils.AtomicSend[utils.Option[data.Anchor]]
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
	anchor := data.Anchor{CommitQC: roadBlock.QC().QC(), AppQC: appQC, Epoch: epoch}
	anchorSend := utils.NewAtomicSend(utils.Some(anchor))
	store := &fakeStateSyncProofBlockStore{
		status:       utils.Some(blockstore.SuffixRange{First: index, NextAppQC: index + 1}),
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
// retained checkpoint, and returns its Anchor. The published Anchor does not change.
func (e *stateSyncProofExportEnv) addNextRoad(rng utils.Rng) data.Anchor {
	qc, blocks := data.TestCommitQC(rng, e.epoch, e.keys, utils.Some(e.roadBlock.QC().QC()))
	roadBlock := types.NewRoadBlock(qc, blocks)
	index := roadBlock.QC().Index()
	proposal := types.NewAppProposal(roadBlock.QC().QC().View(), []byte{4, 5, 6})
	appQC := data.TestAppQC(e.keys, proposal)
	e.store.blocks[index] = roadBlock
	e.store.appProposals[index] = proposal
	e.store.appQCs[index] = appQC
	e.store.status = utils.Some(blockstore.SuffixRange{First: e.index, NextAppQC: index + 1})
	e.checkpoints = []int64{int64(index)}
	return data.Anchor{CommitQC: roadBlock.QC().QC(), AppQC: appQC, Epoch: e.epoch}
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
	status          utils.Option[blockstore.SuffixRange]
	blocks          map[types.RoadIndex]*types.RoadBlock
	appProposals    map[types.RoadIndex]*types.AppProposal
	appQCs          map[types.RoadIndex]*types.AppQC
	readBlockErr    error
	readBlockErrAt  map[types.RoadIndex]error
	readProposalErr error
	readAppQCErr    error
	blockReads      int
}

func (s *fakeStateSyncProofBlockStore) Status() utils.Option[blockstore.SuffixRange] {
	return s.status
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

var _ stateSyncProofBlockStore = (*fakeStateSyncProofBlockStore)(nil)
