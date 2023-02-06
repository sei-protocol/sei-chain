package types

import (
	"bytes"
	"testing"
	"time"

	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	"github.com/stretchr/testify/assert"
	"github.com/tendermint/tendermint/libs/rand"

	"github.com/stretchr/testify/require"
)

func TestValidateGenesisState(t *testing.T) {
	specs := map[string]struct {
		srcMutator func(*GenesisState)
		expError   bool
	}{
		"all good": {
			srcMutator: func(s *GenesisState) {},
		},
		"params invalid": {
			srcMutator: func(s *GenesisState) {
				s.Params = Params{}
			},
			expError: true,
		},
		"codeinfo invalid": {
			srcMutator: func(s *GenesisState) {
				s.Codes[0].CodeInfo.CodeHash = nil
			},
			expError: true,
		},
		"contract invalid": {
			srcMutator: func(s *GenesisState) {
				s.Contracts[0].ContractAddress = "invalid"
			},
			expError: true,
		},
		"sequence invalid": {
			srcMutator: func(s *GenesisState) {
				s.Sequences[0].IDKey = nil
			},
			expError: true,
		},
		"genesis store code message invalid": {
			srcMutator: func(s *GenesisState) {
				s.GenMsgs[0].GetStoreCode().WASMByteCode = nil
			},
			expError: true,
		},
		"genesis instantiate contract message invalid": {
			srcMutator: func(s *GenesisState) {
				s.GenMsgs[1].GetInstantiateContract().CodeID = 0
			},
			expError: true,
		},
		"genesis execute contract message invalid": {
			srcMutator: func(s *GenesisState) {
				s.GenMsgs[2].GetExecuteContract().Sender = "invalid"
			},
			expError: true,
		},
		"genesis invalid message type": {
			srcMutator: func(s *GenesisState) {
				s.GenMsgs[0].Sum = nil
			},
			expError: true,
		},
	}
	for msg, spec := range specs {
		t.Run(msg, func(t *testing.T) {
			state := GenesisFixture(spec.srcMutator)
			got := state.ValidateBasic()
			if spec.expError {
				require.Error(t, got)
				return
			}
			require.NoError(t, got)
		})
	}
}

func TestCodeValidateBasic(t *testing.T) {
	specs := map[string]struct {
		srcMutator func(*Code)
		expError   bool
	}{
		"all good": {srcMutator: func(_ *Code) {}},
		"code id invalid": {
			srcMutator: func(c *Code) {
				c.CodeID = 0
			},
			expError: true,
		},
		"codeinfo invalid": {
			srcMutator: func(c *Code) {
				c.CodeInfo.CodeHash = nil
			},
			expError: true,
		},
		"codeBytes empty": {
			srcMutator: func(c *Code) {
				c.CodeBytes = []byte{}
			},
			expError: true,
		},
		"codeBytes nil": {
			srcMutator: func(c *Code) {
				c.CodeBytes = nil
			},
			expError: true,
		},
		"codeBytes greater limit": {
			srcMutator: func(c *Code) {
				c.CodeBytes = bytes.Repeat([]byte{0x1}, MaxWasmSize+1)
			},
			expError: true,
		},
	}
	for msg, spec := range specs {
		t.Run(msg, func(t *testing.T) {
			state := CodeFixture(spec.srcMutator)
			got := state.ValidateBasic()
			if spec.expError {
				require.Error(t, got)
				return
			}
			require.NoError(t, got)
		})
	}
}

func TestContractValidateBasic(t *testing.T) {
	specs := map[string]struct {
		srcMutator func(*Contract)
		expError   bool
	}{
		"all good": {srcMutator: func(_ *Contract) {}},
		"contract address invalid": {
			srcMutator: func(c *Contract) {
				c.ContractAddress = "invalid"
			},
			expError: true,
		},
		"contract info invalid": {
			srcMutator: func(c *Contract) {
				c.ContractInfo.Creator = "invalid"
			},
			expError: true,
		},
		"contract with created set": {
			srcMutator: func(c *Contract) {
				c.ContractInfo.Created = &AbsoluteTxPosition{}
			},
			expError: true,
		},
		"contract state invalid": {
			srcMutator: func(c *Contract) {
				c.ContractState = append(c.ContractState, Model{})
			},
			expError: true,
		},
	}
	for msg, spec := range specs {
		t.Run(msg, func(t *testing.T) {
			state := ContractFixture(spec.srcMutator)
			got := state.ValidateBasic()
			if spec.expError {
				require.Error(t, got)
				return
			}
			require.NoError(t, got)
		})
	}
}

func TestGenesisContractInfoMarshalUnmarshal(t *testing.T) {
	var myAddr sdk.AccAddress = rand.Bytes(ContractAddrLen)
	var myOtherAddr sdk.AccAddress = rand.Bytes(ContractAddrLen)
	anyPos := AbsoluteTxPosition{BlockHeight: 1, TxIndex: 2}

	anyTime := time.Now().UTC()
	// using gov proposal here as a random protobuf types as it contains an Any type inside for nested unpacking
	myExtension, err := govtypes.NewProposal(&govtypes.TextProposal{Title: "bar"}, 1, anyTime, anyTime)
	require.NoError(t, err)
	myExtension.TotalDeposit = nil

	src := NewContractInfo(1, myAddr, myOtherAddr, "bar", &anyPos)
	err = src.SetExtension(&myExtension)
	require.NoError(t, err)

	interfaceRegistry := types.NewInterfaceRegistry()
	marshaler := codec.NewProtoCodec(interfaceRegistry)
	RegisterInterfaces(interfaceRegistry)
	// register proposal as extension type
	interfaceRegistry.RegisterImplementations(
		(*ContractInfoExtension)(nil),
		&govtypes.Proposal{},
	)
	// register gov types for nested Anys
	govtypes.RegisterInterfaces(interfaceRegistry)

	// when encode
	gs := GenesisState{
		Contracts: []Contract{{
			ContractInfo: src,
		}},
	}

	bz, err := marshaler.Marshal(&gs)
	require.NoError(t, err)
	// and decode
	var destGs GenesisState
	err = marshaler.Unmarshal(bz, &destGs)
	require.NoError(t, err)
	// then
	require.Len(t, destGs.Contracts, 1)
	dest := destGs.Contracts[0].ContractInfo
	assert.Equal(t, src, dest)
	// and sanity check nested any
	var destExt govtypes.Proposal
	require.NoError(t, dest.ReadExtension(&destExt))
	assert.Equal(t, destExt.GetTitle(), "bar")
}
