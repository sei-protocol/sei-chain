package evidence_test

import (
	"math"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/sei-protocol/sei-chain/precompiles/evidence"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	evidencetypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/evidence/types"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/stretchr/testify/require"
)

func TestEvidencePrecompileAddress(t *testing.T) {
	testApp := testkeeper.EVMTestApp
	p, err := evidence.NewPrecompile(testApp.GetPrecompileKeepers())
	require.NoError(t, err)
	require.Equal(t, common.HexToAddress(evidence.EvidenceAddress), p.Address())
}

func TestEvidenceQueryNotFound(t *testing.T) {
	testApp := testkeeper.EVMTestApp
	ctx := testApp.NewContext(false, tmtypes.Header{}).WithBlockHeight(2)
	k := &testApp.EvmKeeper
	p, err := evidence.NewPrecompile(testApp.GetPrecompileKeepers())
	require.NoError(t, err)
	statedb := state.NewDBImpl(ctx, k, true)
	evm := vm.EVM{StateDB: statedb}

	method, err := p.ABI.MethodById(p.GetExecutor().(*evidence.PrecompileExecutor).EvidenceID)
	require.NoError(t, err)

	inputs, err := method.Inputs.Pack([]byte("nonexistent-evidence-hash"))
	require.NoError(t, err)

	ret, _, err := p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, append(method.ID, inputs...), math.MaxUint64, nil, nil, true, false)
	require.Error(t, err)
	require.Equal(t, vm.ErrExecutionReverted, err)
	require.Nil(t, ret)
}

func TestAllEvidenceEmpty(t *testing.T) {
	testApp := testkeeper.EVMTestApp
	ctx := testApp.NewContext(false, tmtypes.Header{}).WithBlockHeight(2)
	k := &testApp.EvmKeeper
	p, err := evidence.NewPrecompile(testApp.GetPrecompileKeepers())
	require.NoError(t, err)
	statedb := state.NewDBImpl(ctx, k, true)
	evm := vm.EVM{StateDB: statedb}

	method, err := p.ABI.MethodById(p.GetExecutor().(*evidence.PrecompileExecutor).AllEvidenceID)
	require.NoError(t, err)

	inputs, err := method.Inputs.Pack([]byte{})
	require.NoError(t, err)

	ret, _, err := p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, append(method.ID, inputs...), math.MaxUint64, nil, nil, true, false)
	require.NoError(t, err)

	expected, err := method.Outputs.Pack(evidence.AllEvidenceResponse{EvidenceList: [][]byte{}})
	require.NoError(t, err)
	require.Equal(t, expected, ret)
}

func TestEvidenceQueryAndAllEvidence(t *testing.T) {
	testApp := testkeeper.EVMTestApp
	ctx := testApp.NewContext(false, tmtypes.Header{}).WithBlockHeight(2)
	k := &testApp.EvmKeeper
	p, err := evidence.NewPrecompile(testApp.GetPrecompileKeepers())
	require.NoError(t, err)
	statedb := state.NewDBImpl(ctx, k, true)
	evm := vm.EVM{StateDB: statedb}

	ev := &evidencetypes.Equivocation{
		Height:           1,
		Power:            100,
		Time:             time.Unix(1234, 0).UTC(),
		ConsensusAddress: sdk.ConsAddress([]byte("evidenceconsaddr1234")).String(),
	}
	testApp.EvidenceKeeper.SetEvidence(ctx, ev)

	// evidence(bytes evidenceHash)
	method, err := p.ABI.MethodById(p.GetExecutor().(*evidence.PrecompileExecutor).EvidenceID)
	require.NoError(t, err)

	inputs, err := method.Inputs.Pack([]byte(ev.Hash()))
	require.NoError(t, err)

	ret, _, err := p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, append(method.ID, inputs...), math.MaxUint64, nil, nil, true, false)
	require.NoError(t, err)

	outputs, err := method.Outputs.Unpack(ret)
	require.NoError(t, err)
	require.Len(t, outputs, 1)
	evidenceJSON := string(outputs[0].([]byte))
	require.Contains(t, evidenceJSON, "@type")
	require.Contains(t, evidenceJSON, "Equivocation")
	require.Contains(t, evidenceJSON, ev.ConsensusAddress)

	// allEvidence(bytes pageKey)
	allMethod, err := p.ABI.MethodById(p.GetExecutor().(*evidence.PrecompileExecutor).AllEvidenceID)
	require.NoError(t, err)

	allInputs, err := allMethod.Inputs.Pack([]byte{})
	require.NoError(t, err)

	allRet, _, err := p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, append(allMethod.ID, allInputs...), math.MaxUint64, nil, nil, true, false)
	require.NoError(t, err)

	expected, err := allMethod.Outputs.Pack(evidence.AllEvidenceResponse{EvidenceList: [][]byte{[]byte(evidenceJSON)}})
	require.NoError(t, err)
	require.Equal(t, expected, allRet)
}

func TestEvidenceQueryNonPayable(t *testing.T) {
	testApp := testkeeper.EVMTestApp
	ctx := testApp.NewContext(false, tmtypes.Header{}).WithBlockHeight(2)
	k := &testApp.EvmKeeper
	p, err := evidence.NewPrecompile(testApp.GetPrecompileKeepers())
	require.NoError(t, err)
	statedb := state.NewDBImpl(ctx, k, true)
	evm := vm.EVM{StateDB: statedb}

	method, err := p.ABI.MethodById(p.GetExecutor().(*evidence.PrecompileExecutor).AllEvidenceID)
	require.NoError(t, err)

	inputs, err := method.Inputs.Pack([]byte{})
	require.NoError(t, err)

	ret, _, err := p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, append(method.ID, inputs...), math.MaxUint64, big.NewInt(1), nil, false, false)
	require.Error(t, err)
	require.Equal(t, vm.ErrExecutionReverted, err)
	require.Nil(t, ret)
}
