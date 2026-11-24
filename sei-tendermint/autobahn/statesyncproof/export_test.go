package statesyncproof

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	dto "github.com/prometheus/client_model/go"
	"google.golang.org/protobuf/encoding/protowire"

	"github.com/sei-protocol/sei-chain/giga/apphash"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func TestExporterWritesPersistedCheckpoint(t *testing.T) {
	rng := utils.TestRng()
	env := newExportEnv(t, rng)

	require.NoError(t, env.exporter.Sweep())

	decoded := env.readProof(t, env.height)
	require.Equal(t, env.height, decoded.Height())
	require.Equal(t, env.block.Header().Hash(), decoded.RoadBlock().Header().Hash())
	require.Equal(t, env.appQC.Proposal().Height(), decoded.AppQC().Proposal().Height())
	require.Equal(t, env.appHashData, decoded.AppHashData())
}

func TestExporterWaitsForAppHashData(t *testing.T) {
	rng := utils.TestRng()
	env := newExportEnv(t, rng)
	delete(env.appHashRecords, env.height)

	require.NoError(t, env.exporter.Sweep())
	require.NoFileExists(t, env.proofPath(env.height))

	env.appHashRecords[env.height] = env.appHashData
	require.NoError(t, env.exporter.Sweep())
	require.FileExists(t, env.proofPath(env.height))
}

func TestExporterWaitsForAppQC(t *testing.T) {
	rng := utils.TestRng()
	env := newExportEnv(t, rng)
	env.lastAppQC = utils.None[types.RoadHeight]()

	require.NoError(t, env.exporter.Sweep())
	require.NoFileExists(t, env.proofPath(env.height))

	env.lastAppQC = utils.Some(env.height)
	require.NoError(t, env.exporter.Sweep())
	require.FileExists(t, env.proofPath(env.height))
}

func TestExporterWaitsForAppQCAtCheckpoint(t *testing.T) {
	rng := utils.TestRng()
	env := newExportEnv(t, rng)
	next := env.addNextRoad(rng)
	nextHeight := next.Proposal().Height()

	require.NoError(t, env.exporter.Sweep())
	require.NoFileExists(t, env.proofPath(nextHeight))

	env.lastAppQC = utils.Some(nextHeight)
	require.NoError(t, env.exporter.Sweep())
	require.FileExists(t, env.proofPath(nextHeight))
}

func TestExporterCatchesCheckpointAfterAppQC(t *testing.T) {
	rng := utils.TestRng()
	env := newExportEnv(t, rng)
	env.checkpoints = nil

	require.NoError(t, env.exporter.Sweep())
	require.NoFileExists(t, env.proofPath(env.height))

	env.checkpoints = []types.RoadHeight{env.height}
	require.NoError(t, env.exporter.Sweep())
	require.FileExists(t, env.proofPath(env.height))
}

func TestExporterSkipsMissingOrPrunedInputs(t *testing.T) {
	cases := map[string]func(*exportEnv){
		"pruned block": func(env *exportEnv) {
			env.store.readBlockErr = types.ErrPruned
		},
		"missing block": func(env *exportEnv) {
			delete(env.store.blocks, env.height)
		},
		"pruned AppProposal": func(env *exportEnv) {
			env.store.readProposalErr = types.ErrPruned
		},
		"missing AppProposal": func(env *exportEnv) {
			delete(env.store.appProposals, env.height)
		},
		"pruned AppQC": func(env *exportEnv) {
			env.store.readAppQCErr = types.ErrPruned
		},
		"missing AppQC": func(env *exportEnv) {
			delete(env.store.appQCs, env.height)
		},
		"pruned app hash data": func(env *exportEnv) {
			env.appHashDataErr = fmt.Errorf("app hash data: %w", types.ErrPruned)
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			rng := utils.TestRng()
			env := newExportEnv(t, rng)
			mutate(env)

			require.NoError(t, env.exporter.Sweep())
			require.NoFileExists(t, env.proofPath(env.height))
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
		"app hash data": func(env *exportEnv) {
			env.appHashDataErr = errors.New("app hash data read failed")
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			rng := utils.TestRng()
			env := newExportEnv(t, rng)
			mutate(env)

			require.Error(t, env.exporter.Sweep())
			require.NoFileExists(t, env.proofPath(env.height))
		})
	}
}

func TestExporterSkipsInconsistentRecords(t *testing.T) {
	cases := map[string]func(*exportEnv, utils.Rng){
		"AppHash mismatch": func(env *exportEnv, _ utils.Rng) {
			env.store.appProposals[env.height] = types.NewAppProposal(env.block.QC().QC().View(), []byte{9, 9, 9})
		},
		"AppQC of another road": func(env *exportEnv, rng utils.Rng) {
			next := env.addNextRoad(rng)
			nextHeight := next.Proposal().Height()
			env.store.appProposals[env.height] = env.store.appProposals[nextHeight]
			env.store.appQCs[env.height] = next
			env.checkpoints = []types.RoadHeight{env.height}
		},
		"AppQC of another epoch": func(env *exportEnv, _ utils.Rng) {
			view := env.block.QC().QC().View()
			view.EpochIndex++
			proposal := types.NewAppProposal(view, env.appQC.Proposal().AppHash())
			env.store.appQCs[env.height] = data.TestAppQC(env.keys, proposal)
		},
		"road block of another road": func(env *exportEnv, rng utils.Rng) {
			next := env.addNextRoad(rng)
			env.store.blocks[env.height] = env.store.blocks[next.Proposal().Height()]
			env.checkpoints = []types.RoadHeight{env.height}
		},
		"app hash data the AppQC does not sign": func(env *exportEnv, rng utils.Rng) {
			env.appHashRecords[env.height] = genAppHashData(rng, env.height)
		},
		"app hash data of another road": func(env *exportEnv, rng utils.Rng) {
			env.signAppHashData(genAppHashData(rng, env.height+1))
		},
	}
	for name, corrupt := range cases {
		t.Run(name, func(t *testing.T) {
			rng := utils.TestRng()
			env := newExportEnv(t, rng)
			corrupt(env, rng)
			failures, inconsistent := exportFailureCount(t), inconsistentRecordCount(t)

			require.NoError(t, env.exporter.Sweep())
			require.NoFileExists(t, env.proofPath(env.height))
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
	require.FileExists(t, env.proofPath(env.height))
}

func TestExporterKeepsValidExistingProof(t *testing.T) {
	rng := utils.TestRng()
	env := newExportEnv(t, rng)
	old, err := Encode(env.block, env.appQC, env.appHashData)
	require.NoError(t, err)
	// An unknown field keeps the file valid and makes its bytes differ from a fresh export.
	old = protowire.AppendTag(old, 99, protowire.BytesType)
	old = protowire.AppendBytes(old, []byte("old"))
	env.writeProof(t, env.height, old)

	require.NoError(t, env.exporter.Sweep())

	got, err := os.ReadFile(env.proofPath(env.height))
	require.NoError(t, err)
	require.True(t, bytes.Equal(old, got))
}

func TestExporterProofSurvivesLossOfUnflushedRecords(t *testing.T) {
	rng := utils.TestRng()
	env := newExportEnv(t, rng)
	require.NoError(t, env.exporter.Sweep())
	written, err := os.ReadFile(env.proofPath(env.height))
	require.NoError(t, err)

	// A crash before BlockStore.Flush loses the records of the road, and LastAppQC moves back.
	delete(env.store.blocks, env.height)
	delete(env.store.appProposals, env.height)
	delete(env.store.appQCs, env.height)
	env.lastAppQC = utils.None[types.RoadHeight]()
	failures, inconsistent := exportFailureCount(t), inconsistentRecordCount(t)
	env.exporter = env.newExporter()

	require.NoError(t, env.exporter.Sweep())

	got, err := os.ReadFile(env.proofPath(env.height))
	require.NoError(t, err)
	require.True(t, bytes.Equal(written, got))
	proof, err := Decode(got, env.height)
	require.NoError(t, err)
	require.NoError(t, proof.RoadBlock().Verify(env.epoch))
	require.NoError(t, proof.AppQC().Verify(env.epoch.Committee()))
	require.Equal(t, failures, exportFailureCount(t))
	require.Equal(t, inconsistent, inconsistentRecordCount(t))
}

func TestExporterReplacesInvalidProofAtStartup(t *testing.T) {
	cases := map[string]func(*exportEnv, utils.Rng) []byte{
		"garbage": func(*exportEnv, utils.Rng) []byte {
			return []byte("garbage")
		},
		"wrong road": func(env *exportEnv, rng utils.Rng) []byte {
			next := env.addNextRoad(rng)
			nextHeight := next.Proposal().Height()
			encoded, err := Encode(env.store.blocks[nextHeight], next, env.appHashRecords[nextHeight])
			require.NoError(t, err)
			env.checkpoints = []types.RoadHeight{env.height}
			return encoded
		},
		"no app hash data": func(env *exportEnv, _ utils.Rng) []byte {
			return frame(&pb.StateSyncProof{
				CommitQc: types.FullCommitQCConv.Encode(env.block.QC()),
				Blocks:   types.LaneBlockConv.EncodeSlice(env.block.LaneBlocks()),
				AppQc:    types.AppQCConv.Encode(env.appQC),
			})
		},
	}
	for name, invalid := range cases {
		t.Run(name, func(t *testing.T) {
			rng := utils.TestRng()
			env := newExportEnv(t, rng)
			env.writeProof(t, env.height, invalid(env, rng))

			require.NoError(t, env.exporter.Sweep())

			decoded := env.readProof(t, env.height)
			require.Equal(t, env.block.Header().Hash(), decoded.RoadBlock().Header().Hash())
		})
	}
}

func TestExporterRemovesInvalidProofOfPrunedCheckpoint(t *testing.T) {
	rng := utils.TestRng()
	env := newExportEnv(t, rng)
	env.store.readBlockErr = types.ErrPruned
	env.writeProof(t, env.height, []byte("garbage"))

	require.NoError(t, env.exporter.Sweep())
	require.NoFileExists(t, env.proofPath(env.height))
}

func TestExporterRewritesDeletedProofOnNextCheckpoint(t *testing.T) {
	rng := utils.TestRng()
	env := newExportEnv(t, rng)
	require.NoError(t, env.exporter.Sweep())
	require.NoError(t, os.Remove(env.proofPath(env.height)))

	env.checkpoints = append(env.checkpoints, env.height+1)
	require.NoError(t, env.exporter.Sweep())
	require.FileExists(t, env.proofPath(env.height))
}

func TestExporterRemovesOrphansAndTempFiles(t *testing.T) {
	rng := utils.TestRng()
	env := newExportEnv(t, rng)
	orphan := env.proofPath(env.height + 1)
	tmp := env.proofPath(env.height) + ".tmp"
	require.NoError(t, os.MkdirAll(env.dir, 0o750))
	require.NoError(t, os.WriteFile(orphan, []byte("orphan"), 0o600))
	require.NoError(t, os.WriteFile(tmp, []byte("tmp"), 0o600))

	require.NoError(t, env.exporter.Sweep())

	require.NoFileExists(t, orphan)
	require.NoFileExists(t, tmp)
	require.FileExists(t, env.proofPath(env.height))
}

func TestExporterSkipsSettledCheckpoints(t *testing.T) {
	cases := map[string]func(*exportEnv){
		"exported": func(*exportEnv) {},
		"pruned": func(env *exportEnv) {
			env.store.readBlockErr = types.ErrPruned
		},
		"missing": func(env *exportEnv) {
			delete(env.store.blocks, env.height)
		},
		"inconsistent": func(env *exportEnv) {
			env.store.appProposals[env.height] = types.NewAppProposal(env.block.QC().QC().View(), []byte{9, 9, 9})
		},
		"pruned app hash data": func(env *exportEnv) {
			env.appHashDataErr = fmt.Errorf("app hash data: %w", types.ErrPruned)
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			rng := utils.TestRng()
			env := newExportEnv(t, rng)
			mutate(env)

			require.NoError(t, env.exporter.Sweep())
			blockReads := env.store.blockReads
			require.NoError(t, env.exporter.Sweep())
			require.Equal(t, blockReads, env.store.blockReads)
		})
	}
}

func TestExporterReadErrorDoesNotBlockOtherCheckpoints(t *testing.T) {
	rng := utils.TestRng()
	env := newExportEnv(t, rng)
	next := env.addNextRoad(rng)
	nextHeight := next.Proposal().Height()
	env.checkpoints = []types.RoadHeight{env.height, nextHeight}
	env.lastAppQC = utils.Some(nextHeight)
	env.store.readBlockErrAt = map[types.RoadHeight]error{env.height: errors.New("disk failed")}

	require.Error(t, env.exporter.Sweep())
	require.NoFileExists(t, env.proofPath(env.height))
	require.FileExists(t, env.proofPath(nextHeight))

	env.store.readBlockErrAt = nil
	require.NoError(t, env.exporter.Sweep())
	require.FileExists(t, env.proofPath(env.height))
}

func TestExporterStuckFileDoesNotBlockOtherCheckpoints(t *testing.T) {
	cases := map[string]func(env *exportEnv, nextHeight types.RoadHeight) types.RoadHeight{
		"unreadable proof file": func(env *exportEnv, _ types.RoadHeight) types.RoadHeight {
			return env.height
		},
		"unremovable orphan": func(_ *exportEnv, nextHeight types.RoadHeight) types.RoadHeight {
			return nextHeight + 1
		},
	}
	for name, stuckIndex := range cases {
		t.Run(name, func(t *testing.T) {
			rng := utils.TestRng()
			env := newExportEnv(t, rng)
			next := env.addNextRoad(rng)
			nextHeight := next.Proposal().Height()
			env.checkpoints = []types.RoadHeight{env.height, nextHeight}
			env.lastAppQC = utils.Some(nextHeight)
			// A non-empty directory under a proof file name can be neither read nor removed.
			stuck := env.proofPath(stuckIndex(env, nextHeight))
			require.NoError(t, os.MkdirAll(filepath.Join(stuck, "child"), 0o750))

			require.Error(t, env.exporter.Sweep())
			require.FileExists(t, env.proofPath(nextHeight))
		})
	}
}

func TestExporterRepeatsTheSameErrorForTheSameFailure(t *testing.T) {
	rng := utils.TestRng()
	env := newExportEnv(t, rng)
	next := env.addNextRoad(rng)
	nextHeight := next.Proposal().Height()
	env.checkpoints = []types.RoadHeight{env.height, nextHeight}
	// A non-empty directory under a proof file name cannot be read, so every startup scan fails on both.
	for _, height := range []types.RoadHeight{env.height, nextHeight} {
		require.NoError(t, os.MkdirAll(filepath.Join(env.proofPath(height), "child"), 0o750))
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
			orphan := env.proofPath(env.height + 1)
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
	require.FileExists(t, env.proofPath(env.height))

	env.checkpoints = nil
	require.NoError(t, env.exporter.Sweep())
	require.NoFileExists(t, env.proofPath(env.height))

	env.checkpoints = []types.RoadHeight{env.height}
	require.NoError(t, env.exporter.Sweep())
	require.FileExists(t, env.proofPath(env.height))
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
	height      types.RoadHeight
	block       *types.RoadBlock
	appQC       *types.AppQC
	appHashData *apphash.AppHashData
	store       *fakeBlockStore
	// lastAppQC is what LastAppQC reports.
	lastAppQC   utils.Option[types.RoadHeight]
	checkpoints []types.RoadHeight
	// appHashRecords holds, by road, the app hash records that AppHashData reports.
	appHashRecords map[types.RoadHeight]*apphash.AppHashData
	// appHashDataErr is the error AppHashData returns, if not nil.
	appHashDataErr error
	exporter       *Exporter
}

func newExportEnv(t *testing.T, rng utils.Rng) *exportEnv {
	t.Helper()
	committee, keys := types.GenCommittee(rng, 4)
	epoch := types.GenEpochWithCommittee(rng, committee)
	qc, blocks := data.TestCommitQC(rng, epoch, keys, utils.None[*types.CommitQC]())
	block := types.NewRoadBlock(qc, blocks)
	height := block.QC().Height()
	record := genAppHashData(rng, height)
	proposal := appProposalOf(block, record)
	appQC := data.TestAppQC(keys, proposal)
	store := &fakeBlockStore{
		blocks:       map[types.RoadHeight]*types.RoadBlock{height: block},
		appProposals: map[types.RoadHeight]*types.AppProposal{height: proposal},
		appQCs:       map[types.RoadHeight]*types.AppQC{height: appQC},
	}
	env := &exportEnv{
		dir:            t.TempDir(),
		keys:           keys,
		epoch:          epoch,
		height:         height,
		block:          block,
		appQC:          appQC,
		appHashData:    record,
		store:          store,
		lastAppQC:      utils.Some(height),
		checkpoints:    []types.RoadHeight{height},
		appHashRecords: map[types.RoadHeight]*apphash.AppHashData{height: record},
	}
	env.exporter = env.newExporter()
	return env
}

// newExporter returns an Exporter over env's dir, checkpoints, BlockStore and app hash records,
// as a node creates one at startup.
func (e *exportEnv) newExporter() *Exporter {
	return NewExporter(ExportConfig{
		Dir: e.dir,
		CheckpointRoads: func() ([]types.RoadHeight, error) {
			return slices.Clone(e.checkpoints), nil
		},
		BlockStore: e.store,
		LastAppQC: func() utils.Option[types.RoadHeight] {
			return e.lastAppQC
		},
		AppHashData: func(height types.RoadHeight) (utils.Option[*apphash.AppHashData], error) {
			if e.appHashDataErr != nil {
				return utils.None[*apphash.AppHashData](), e.appHashDataErr
			}
			record, ok := e.appHashRecords[height]
			if !ok {
				return utils.None[*apphash.AppHashData](), nil
			}
			return utils.Some(record), nil
		},
	})
}

// addNextRoad stores the road after env.index with its AppProposal, AppQC and app hash record,
// makes it the only retained checkpoint, and returns its AppQC. LastAppQC does not change.
func (e *exportEnv) addNextRoad(rng utils.Rng) *types.AppQC {
	qc, blocks := data.TestCommitQC(rng, e.epoch, e.keys, utils.Some(e.block.QC().QC()))
	block := types.NewRoadBlock(qc, blocks)
	height := block.QC().Height()
	record := genAppHashData(rng, height)
	proposal := appProposalOf(block, record)
	appQC := data.TestAppQC(e.keys, proposal)
	e.store.blocks[height] = block
	e.store.appProposals[height] = proposal
	e.store.appQCs[height] = appQC
	e.appHashRecords[height] = record
	e.checkpoints = []types.RoadHeight{height}
	return appQC
}

// signAppHashData makes record the app hash record of env.height, and replaces the AppProposal and
// AppQC of env.height with ones over its app hash.
func (e *exportEnv) signAppHashData(record *apphash.AppHashData) {
	proposal := appProposalOf(e.block, record)
	e.store.appProposals[e.height] = proposal
	e.store.appQCs[e.height] = data.TestAppQC(e.keys, proposal)
	e.appHashRecords[e.height] = record
}

// appProposalOf returns the AppProposal of the road of block over the app hash of record.
func appProposalOf(block *types.RoadBlock, record *apphash.AppHashData) *types.AppProposal {
	appHash := record.AppHash()
	return types.NewAppProposal(block.QC().QC().View(), appHash[:])
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

func (e *exportEnv) proofPath(height types.RoadHeight) string {
	return filepath.Join(e.dir, FileName(height))
}

func (e *exportEnv) writeProof(t *testing.T, height types.RoadHeight, data []byte) {
	t.Helper()
	require.NoError(t, os.MkdirAll(e.dir, 0o750))
	require.NoError(t, os.WriteFile(e.proofPath(height), data, 0o600))
}

func (e *exportEnv) readProof(t *testing.T, height types.RoadHeight) *Proof {
	t.Helper()
	encoded, err := os.ReadFile(e.proofPath(height))
	require.NoError(t, err)
	decoded, err := Decode(encoded, height)
	require.NoError(t, err)
	return decoded
}

type fakeBlockStore struct {
	blocks          map[types.RoadHeight]*types.RoadBlock
	appProposals    map[types.RoadHeight]*types.AppProposal
	appQCs          map[types.RoadHeight]*types.AppQC
	readBlockErr    error
	readBlockErrAt  map[types.RoadHeight]error
	readProposalErr error
	readAppQCErr    error
	blockReads      int
}

func (s *fakeBlockStore) ReadBlock(height types.RoadHeight) (utils.Option[*types.RoadBlock], error) {
	s.blockReads++
	if s.readBlockErr != nil {
		return utils.None[*types.RoadBlock](), s.readBlockErr
	}
	if err, ok := s.readBlockErrAt[height]; ok {
		return utils.None[*types.RoadBlock](), err
	}
	block, ok := s.blocks[height]
	if !ok {
		return utils.None[*types.RoadBlock](), nil
	}
	return utils.Some(block), nil
}

func (s *fakeBlockStore) ReadAppProposal(height types.RoadHeight) (utils.Option[*types.AppProposal], error) {
	if s.readProposalErr != nil {
		return utils.None[*types.AppProposal](), s.readProposalErr
	}
	proposal, ok := s.appProposals[height]
	if !ok {
		return utils.None[*types.AppProposal](), nil
	}
	return utils.Some(proposal), nil
}

func (s *fakeBlockStore) ReadAppQC(height types.RoadHeight) (utils.Option[*types.AppQC], error) {
	if s.readAppQCErr != nil {
		return utils.None[*types.AppQC](), s.readAppQCErr
	}
	appQC, ok := s.appQCs[height]
	if !ok {
		return utils.None[*types.AppQC](), nil
	}
	return utils.Some(appQC), nil
}

var _ BlockStore = (*fakeBlockStore)(nil)
