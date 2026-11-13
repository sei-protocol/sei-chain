package statesyncproof

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	dto "github.com/prometheus/client_model/go"
	"google.golang.org/protobuf/encoding/protowire"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func TestExporterWritesPersistedCheckpoint(t *testing.T) {
	rng := utils.TestRng()
	env := newExportEnv(t, rng)

	require.NoError(t, env.exporter.Sweep())

	decoded := env.readProof(t, env.index)
	require.Equal(t, env.index, decoded.Index())
	require.Equal(t, env.roadBlock.Header().Hash(), decoded.RoadBlock().Header().Hash())
	require.Equal(t, env.appQC.Proposal().Index(), decoded.AppQC().Proposal().Index())
}

func TestExporterWaitsForDurableAppQC(t *testing.T) {
	rng := utils.TestRng()
	env := newExportEnv(t, rng)
	env.anchorSend.Store(data.TestAnchor(env.epoch))

	require.NoError(t, env.exporter.Sweep())
	require.NoFileExists(t, env.proofPath(env.index))

	env.anchorSend.Store(env.anchorAt(env.anchor))
	require.NoError(t, env.exporter.Sweep())
	require.FileExists(t, env.proofPath(env.index))
}

func TestExporterWaitsForDurableAppQCAtCheckpoint(t *testing.T) {
	rng := utils.TestRng()
	env := newExportEnv(t, rng)
	next := env.addNextRoad(rng)
	nextIndex := next.AppQC.Proposal().Index()

	require.NoError(t, env.exporter.Sweep())
	require.NoFileExists(t, env.proofPath(nextIndex))

	env.anchorSend.Store(env.anchorAt(next))
	require.NoError(t, env.exporter.Sweep())
	require.FileExists(t, env.proofPath(nextIndex))
}

func TestExporterCatchesCheckpointAfterAppQC(t *testing.T) {
	rng := utils.TestRng()
	env := newExportEnv(t, rng)
	env.checkpoints = nil

	require.NoError(t, env.exporter.Sweep())
	require.NoFileExists(t, env.proofPath(env.index))

	env.checkpoints = []int64{int64(env.index)}
	require.NoError(t, env.exporter.Sweep())
	require.FileExists(t, env.proofPath(env.index))
}

func TestExporterSkipsLastRoad(t *testing.T) {
	rng := utils.TestRng()
	env := newExportEnv(t, rng)
	env.closingRoad = true

	require.NoError(t, env.exporter.Sweep())
	require.NoFileExists(t, env.proofPath(env.index))
}

func TestExporterSkipsMissingOrPrunedInputs(t *testing.T) {
	cases := map[string]func(*exportEnv){
		"pruned block": func(env *exportEnv) {
			env.store.readBlockErr = types.ErrPruned
		},
		"missing block": func(env *exportEnv) {
			delete(env.store.blocks, env.index)
		},
		"pruned AppProposal": func(env *exportEnv) {
			env.store.readProposalErr = types.ErrPruned
		},
		"missing AppProposal": func(env *exportEnv) {
			delete(env.store.appProposals, env.index)
		},
		"pruned AppQC": func(env *exportEnv) {
			env.store.readAppQCErr = types.ErrPruned
		},
		"missing AppQC": func(env *exportEnv) {
			delete(env.store.appQCs, env.index)
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			rng := utils.TestRng()
			env := newExportEnv(t, rng)
			mutate(env)

			require.NoError(t, env.exporter.Sweep())
			require.NoFileExists(t, env.proofPath(env.index))
		})
	}
}

func TestExporterReturnsReadErrors(t *testing.T) {
	cases := map[string]func(*exportEnv){
		"block": func(env *exportEnv) {
			env.store.readBlockErr = errors.New("block read failed")
		},
		"AppProposal": func(env *exportEnv) {
			env.store.readProposalErr = errors.New("proposal read failed")
		},
		"AppQC": func(env *exportEnv) {
			env.store.readAppQCErr = errors.New("app qc read failed")
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			rng := utils.TestRng()
			env := newExportEnv(t, rng)
			mutate(env)

			require.Error(t, env.exporter.Sweep())
			require.NoFileExists(t, env.proofPath(env.index))
		})
	}
}

func TestExporterSkipsInconsistentRecords(t *testing.T) {
	cases := map[string]func(*exportEnv, utils.Rng){
		"AppHash mismatch": func(env *exportEnv, _ utils.Rng) {
			env.store.appProposals[env.index] = types.NewAppProposal(env.roadBlock.QC().QC().View(), []byte{9, 9, 9})
		},
		"AppQC of another road": func(env *exportEnv, rng utils.Rng) {
			next := env.addNextRoad(rng)
			nextIndex := next.AppQC.Proposal().Index()
			env.store.appProposals[env.index] = env.store.appProposals[nextIndex]
			env.store.appQCs[env.index] = next.AppQC
			env.checkpoints = []int64{int64(env.index)}
		},
		"AppQC of another epoch": func(env *exportEnv, _ utils.Rng) {
			view := env.roadBlock.QC().QC().View()
			view.EpochIndex++
			proposal := types.NewAppProposal(view, env.appQC.Proposal().AppHash())
			env.store.appQCs[env.index] = data.TestAppQC(env.keys, proposal)
		},
		"road block of another road": func(env *exportEnv, rng utils.Rng) {
			next := env.addNextRoad(rng)
			env.store.blocks[env.index] = env.store.blocks[next.AppQC.Proposal().Index()]
			env.checkpoints = []int64{int64(env.index)}
		},
	}
	for name, corrupt := range cases {
		t.Run(name, func(t *testing.T) {
			rng := utils.TestRng()
			env := newExportEnv(t, rng)
			corrupt(env, rng)
			failures, inconsistent := exportFailureCount(t), inconsistentRecordCount(t)

			require.NoError(t, env.exporter.Sweep())
			require.NoFileExists(t, env.proofPath(env.index))
			require.Equal(t, failures, exportFailureCount(t))
			require.Equal(t, inconsistent+1, inconsistentRecordCount(t))
		})
	}
}

func TestExporterSweepContinuesOnIOError(t *testing.T) {
	rng := utils.TestRng()
	env := newExportEnv(t, rng)
	env.store.readBlockErr = errors.New("disk failed")
	before := exportFailureCount(t)

	require.Error(t, env.exporter.Sweep())
	require.Error(t, env.exporter.Sweep())
	require.Equal(t, before+2, exportFailureCount(t))

	env.store.readBlockErr = nil
	require.NoError(t, env.exporter.Sweep())
	require.Equal(t, before+2, exportFailureCount(t))
	require.FileExists(t, env.proofPath(env.index))
}

func TestExporterKeepsValidExistingProof(t *testing.T) {
	rng := utils.TestRng()
	env := newExportEnv(t, rng)
	old, err := Encode(env.roadBlock, env.appQC)
	require.NoError(t, err)
	// An unknown field keeps the file valid and makes its bytes differ from a fresh export.
	old = protowire.AppendTag(old, 99, protowire.BytesType)
	old = protowire.AppendBytes(old, []byte("old"))
	env.writeProof(t, env.index, old)

	require.NoError(t, env.exporter.Sweep())

	got, err := os.ReadFile(env.proofPath(env.index))
	require.NoError(t, err)
	require.True(t, bytes.Equal(old, got))
}

func TestExporterReplacesInvalidProofAtStartup(t *testing.T) {
	cases := map[string]func(*exportEnv, utils.Rng) []byte{
		"garbage": func(*exportEnv, utils.Rng) []byte {
			return []byte("garbage")
		},
		"wrong road": func(env *exportEnv, rng utils.Rng) []byte {
			next := env.addNextRoad(rng)
			roadBlock := env.store.blocks[next.AppQC.Proposal().Index()]
			encoded, err := Encode(roadBlock, next.AppQC)
			require.NoError(t, err)
			env.checkpoints = []int64{int64(env.index)}
			return encoded
		},
	}
	for name, invalid := range cases {
		t.Run(name, func(t *testing.T) {
			rng := utils.TestRng()
			env := newExportEnv(t, rng)
			env.writeProof(t, env.index, invalid(env, rng))

			require.NoError(t, env.exporter.Sweep())

			decoded := env.readProof(t, env.index)
			require.Equal(t, env.roadBlock.Header().Hash(), decoded.RoadBlock().Header().Hash())
		})
	}
}

func TestExporterRemovesInvalidProofOfPrunedCheckpoint(t *testing.T) {
	rng := utils.TestRng()
	env := newExportEnv(t, rng)
	env.store.readBlockErr = types.ErrPruned
	env.writeProof(t, env.index, []byte("garbage"))

	require.NoError(t, env.exporter.Sweep())
	require.NoFileExists(t, env.proofPath(env.index))
}

func TestExporterRewritesDeletedProofOnNextCheckpoint(t *testing.T) {
	rng := utils.TestRng()
	env := newExportEnv(t, rng)
	require.NoError(t, env.exporter.Sweep())
	require.NoError(t, os.Remove(env.proofPath(env.index)))

	env.checkpoints = append(env.checkpoints, int64(env.index)+1)
	require.NoError(t, env.exporter.Sweep())
	require.FileExists(t, env.proofPath(env.index))
}

func TestExporterRemovesOrphansAndTempFiles(t *testing.T) {
	rng := utils.TestRng()
	env := newExportEnv(t, rng)
	orphan := env.proofPath(env.index + 1)
	tmp := env.proofPath(env.index) + ".tmp"
	require.NoError(t, os.MkdirAll(env.dir, 0o750))
	require.NoError(t, os.WriteFile(orphan, []byte("orphan"), 0o600))
	require.NoError(t, os.WriteFile(tmp, []byte("tmp"), 0o600))

	require.NoError(t, env.exporter.Sweep())

	require.NoFileExists(t, orphan)
	require.NoFileExists(t, tmp)
	require.FileExists(t, env.proofPath(env.index))
}

func TestExporterSkipsSettledCheckpoints(t *testing.T) {
	cases := map[string]func(*exportEnv){
		"exported": func(*exportEnv) {},
		"pruned": func(env *exportEnv) {
			env.store.readBlockErr = types.ErrPruned
		},
		"missing": func(env *exportEnv) {
			delete(env.store.blocks, env.index)
		},
		"inconsistent": func(env *exportEnv) {
			env.store.appProposals[env.index] = types.NewAppProposal(env.roadBlock.QC().QC().View(), []byte{9, 9, 9})
		},
		"last road": func(env *exportEnv) {
			env.closingRoad = true
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			rng := utils.TestRng()
			env := newExportEnv(t, rng)
			mutate(env)

			require.NoError(t, env.exporter.Sweep())
			blockReads, closingChecks := env.store.blockReads, env.closingChecks
			require.NoError(t, env.exporter.Sweep())
			require.Equal(t, blockReads, env.store.blockReads)
			require.Equal(t, closingChecks, env.closingChecks)
		})
	}
}

func TestExporterReadErrorDoesNotBlockOtherCheckpoints(t *testing.T) {
	rng := utils.TestRng()
	env := newExportEnv(t, rng)
	next := env.addNextRoad(rng)
	nextIndex := next.AppQC.Proposal().Index()
	env.checkpoints = []int64{int64(env.index), int64(nextIndex)}
	env.anchorSend.Store(env.anchorAt(next))
	env.store.readBlockErrAt = map[types.RoadIndex]error{env.index: errors.New("disk failed")}

	require.Error(t, env.exporter.Sweep())
	require.NoFileExists(t, env.proofPath(env.index))
	require.FileExists(t, env.proofPath(nextIndex))

	env.store.readBlockErrAt = nil
	require.NoError(t, env.exporter.Sweep())
	require.FileExists(t, env.proofPath(env.index))
}

func TestExporterStuckFileDoesNotBlockOtherCheckpoints(t *testing.T) {
	cases := map[string]func(env *exportEnv, nextIndex types.RoadIndex) types.RoadIndex{
		"unreadable proof file": func(env *exportEnv, _ types.RoadIndex) types.RoadIndex {
			return env.index
		},
		"unremovable orphan": func(_ *exportEnv, nextIndex types.RoadIndex) types.RoadIndex {
			return nextIndex + 1
		},
	}
	for name, stuckIndex := range cases {
		t.Run(name, func(t *testing.T) {
			rng := utils.TestRng()
			env := newExportEnv(t, rng)
			next := env.addNextRoad(rng)
			nextIndex := next.AppQC.Proposal().Index()
			env.checkpoints = []int64{int64(env.index), int64(nextIndex)}
			env.anchorSend.Store(env.anchorAt(next))
			// A non-empty directory under a proof file name can be neither read nor removed.
			stuck := env.proofPath(stuckIndex(env, nextIndex))
			require.NoError(t, os.MkdirAll(filepath.Join(stuck, "child"), 0o750))

			require.Error(t, env.exporter.Sweep())
			require.FileExists(t, env.proofPath(nextIndex))
		})
	}
}

func TestExporterRepeatsTheSameErrorForTheSameFailure(t *testing.T) {
	rng := utils.TestRng()
	env := newExportEnv(t, rng)
	next := env.addNextRoad(rng)
	nextIndex := next.AppQC.Proposal().Index()
	env.checkpoints = []int64{int64(env.index), int64(nextIndex)}
	// A non-empty directory under a proof file name cannot be read, so every startup scan fails on both.
	for _, index := range []types.RoadIndex{env.index, nextIndex} {
		require.NoError(t, os.MkdirAll(filepath.Join(env.proofPath(index), "child"), 0o750))
	}

	first := env.exporter.Sweep()
	require.Error(t, first)
	for range 20 {
		err := env.exporter.Sweep()
		require.Error(t, err)
		require.Equal(t, first.Error(), err.Error())
	}
}

func TestExporterRetriesFailedCleanup(t *testing.T) {
	for name, scannedBefore := range map[string]bool{"first scan": false, "later scan": true} {
		t.Run(name, func(t *testing.T) {
			rng := utils.TestRng()
			env := newExportEnv(t, rng)
			if scannedBefore {
				require.NoError(t, env.exporter.Sweep())
				env.checkpoints = nil
			}
			// A non-empty directory under a proof file name cannot be removed until it is emptied.
			orphan := env.proofPath(env.index + 1)
			require.NoError(t, os.MkdirAll(filepath.Join(orphan, "child"), 0o750))

			require.Error(t, env.exporter.Sweep())
			_, err := os.Stat(orphan)
			require.NoError(t, err)

			require.NoError(t, os.Remove(filepath.Join(orphan, "child")))
			require.NoError(t, env.exporter.Sweep())
			_, err = os.Stat(orphan)
			require.ErrorIs(t, err, os.ErrNotExist)
		})
	}
}

func TestExporterForgetsDroppedCheckpoints(t *testing.T) {
	rng := utils.TestRng()
	env := newExportEnv(t, rng)

	require.NoError(t, env.exporter.Sweep())
	require.FileExists(t, env.proofPath(env.index))

	env.checkpoints = nil
	require.NoError(t, env.exporter.Sweep())
	require.NoFileExists(t, env.proofPath(env.index))

	env.checkpoints = []int64{int64(env.index)}
	require.NoError(t, env.exporter.Sweep())
	require.FileExists(t, env.proofPath(env.index))
}

func TestWriteFileRemovesTempFileWhenRenameFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "target")
	require.NoError(t, os.Mkdir(path, 0o750))

	require.Error(t, writeFile(path, []byte("data")))

	require.NoFileExists(t, path+".tmp")
}

type exportEnv struct {
	dir         string
	keys        []types.SecretKey
	epoch       *types.Epoch
	index       types.RoadIndex
	roadBlock   *types.RoadBlock
	appQC       *types.AppQC
	store       *fakeBlockStore
	anchor      data.AnchorFirst
	anchorSend  *utils.AtomicSend[data.Anchor]
	checkpoints []int64
	// closingRoad is what IsClosingRoad reports for every road.
	closingRoad bool
	// closingChecks counts the IsClosingRoad calls.
	closingChecks int
	exporter      *Exporter
}

func newExportEnv(t *testing.T, rng utils.Rng) *exportEnv {
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
	store := &fakeBlockStore{
		blocks:       map[types.RoadIndex]*types.RoadBlock{index: roadBlock},
		appProposals: map[types.RoadIndex]*types.AppProposal{index: proposal},
		appQCs:       map[types.RoadIndex]*types.AppQC{index: appQC},
	}
	env := &exportEnv{
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
	env.exporter = NewExporter(ExportConfig{
		Dir: env.dir,
		CheckpointHeights: func() ([]int64, error) {
			return slices.Clone(env.checkpoints), nil
		},
		BlockStore: store,
		Anchor:     anchorSend.Subscribe(),
		IsClosingRoad: func(types.RoadIndex) bool {
			env.closingChecks++
			return env.closingRoad
		},
	})
	return env
}

// addNextRoad stores the road after env.index with its AppProposal and AppQC, makes it the only
// retained checkpoint, and returns its AnchorFirst. The published Anchor does not change.
func (e *exportEnv) addNextRoad(rng utils.Rng) data.AnchorFirst {
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
func (e *exportEnv) anchorAt(first data.AnchorFirst) data.Anchor {
	anchor := data.TestAnchor(e.epoch)
	anchor.First = utils.Some(first)
	return anchor
}

func exportFailureCount(t *testing.T) float64 {
	t.Helper()
	var m dto.Metric
	require.NoError(t, Global.exportFailuresAt().Write(&m))
	return m.GetCounter().GetValue()
}

func inconsistentRecordCount(t *testing.T) float64 {
	t.Helper()
	var m dto.Metric
	require.NoError(t, Global.inconsistentRecordsAt().Write(&m))
	return m.GetCounter().GetValue()
}

func (e *exportEnv) proofPath(index types.RoadIndex) string {
	return filepath.Join(e.dir, FileName(index))
}

func (e *exportEnv) writeProof(t *testing.T, index types.RoadIndex, data []byte) {
	t.Helper()
	require.NoError(t, os.MkdirAll(e.dir, 0o750))
	require.NoError(t, os.WriteFile(e.proofPath(index), data, 0o600))
}

func (e *exportEnv) readProof(t *testing.T, index types.RoadIndex) *Proof {
	t.Helper()
	encoded, err := os.ReadFile(e.proofPath(index))
	require.NoError(t, err)
	decoded, err := Decode(encoded, index)
	require.NoError(t, err)
	return decoded
}

type fakeBlockStore struct {
	blocks          map[types.RoadIndex]*types.RoadBlock
	appProposals    map[types.RoadIndex]*types.AppProposal
	appQCs          map[types.RoadIndex]*types.AppQC
	readBlockErr    error
	readBlockErrAt  map[types.RoadIndex]error
	readProposalErr error
	readAppQCErr    error
	blockReads      int
}

func (s *fakeBlockStore) ReadBlock(index types.RoadIndex) (utils.Option[*types.RoadBlock], error) {
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

func (s *fakeBlockStore) ReadAppProposal(index types.RoadIndex) (utils.Option[*types.AppProposal], error) {
	if s.readProposalErr != nil {
		return utils.None[*types.AppProposal](), s.readProposalErr
	}
	proposal, ok := s.appProposals[index]
	if !ok {
		return utils.None[*types.AppProposal](), nil
	}
	return utils.Some(proposal), nil
}

func (s *fakeBlockStore) ReadAppQC(index types.RoadIndex) (utils.Option[*types.AppQC], error) {
	if s.readAppQCErr != nil {
		return utils.None[*types.AppQC](), s.readAppQCErr
	}
	appQC, ok := s.appQCs[index]
	if !ok {
		return utils.None[*types.AppQC](), nil
	}
	return utils.Some(appQC), nil
}

var _ BlockStore = (*fakeBlockStore)(nil)
