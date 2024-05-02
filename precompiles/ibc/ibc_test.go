package ibc_test

import (
	"errors"
	"math/big"
	"reflect"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	clienttypes "github.com/cosmos/ibc-go/v3/modules/core/02-client/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	pcommon "github.com/sei-protocol/sei-chain/precompiles/common"
	"github.com/sei-protocol/sei-chain/precompiles/ibc"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/stretchr/testify/require"
	tmtypes "github.com/tendermint/tendermint/proto/tendermint/types"
)

type MockTransferKeeper struct{}

func (tk *MockTransferKeeper) SendTransfer(ctx sdk.Context, sourcePort, sourceChannel string, token sdk.Coin,
	sender sdk.AccAddress, receiver string, timeoutHeight clienttypes.Height, timeoutTimestamp uint64) error {
	return nil
}

type MockFailedTransferTransferKeeper struct{}

func (tk *MockFailedTransferTransferKeeper) SendTransfer(ctx sdk.Context, sourcePort, sourceChannel string, token sdk.Coin,
	sender sdk.AccAddress, receiver string, timeoutHeight clienttypes.Height, timeoutTimestamp uint64) error {
	return errors.New("failed to send transfer")
}

func TestPrecompile_Run(t *testing.T) {
	senderSeiAddress, senderEvmAddress := testkeeper.MockAddressPair()
	receiverAddress := "cosmos1yykwxjzr2tv4mhx5tsf8090sdg96f2ax8fydk2"

	pre, _ := ibc.NewPrecompile(nil, nil, nil, nil, nil)
	testTransfer, _ := pre.ABI.MethodById(pre.TransferID)
	packedTrue, _ := testTransfer.Outputs.Pack(true)

	type fields struct {
		transferKeeper pcommon.TransferKeeper
	}

	type input struct {
		receiverAddr     string
		sourcePort       string
		sourceChannel    string
		denom            string
		amount           *big.Int
		revisionNumber   uint64
		revisionHeight   uint64
		timeoutTimestamp uint64
	}
	type args struct {
		caller          common.Address
		callingContract common.Address
		input           *input
		suppliedGas     uint64
		value           *big.Int
	}

	commonArgs := args{
		caller:          senderEvmAddress,
		callingContract: senderEvmAddress,
		input: &input{
			receiverAddr:     receiverAddress,
			sourcePort:       "transfer",
			sourceChannel:    "channel-0",
			denom:            "denom",
			amount:           big.NewInt(100),
			revisionNumber:   1,
			revisionHeight:   1,
			timeoutTimestamp: 1,
		},
		suppliedGas: uint64(1000000),
		value:       nil,
	}

	tests := []struct {
		name             string
		fields           fields
		args             args
		wantBz           []byte
		wantRemainingGas uint64
		wantErr          bool
		wantErrMsg       string
	}{
		{
			name:             "successful transfer: with amount > 0 between EVM addresses",
			fields:           fields{transferKeeper: &MockTransferKeeper{}},
			args:             commonArgs,
			wantBz:           packedTrue,
			wantRemainingGas: 994040,
			wantErr:          false,
		},
		{
			name:       "failed transfer: internal error",
			fields:     fields{transferKeeper: &MockFailedTransferTransferKeeper{}},
			args:       commonArgs,
			wantBz:     nil,
			wantErr:    true,
			wantErrMsg: "failed to send transfer",
		},
		{
			name:       "failed transfer: caller not whitelisted",
			fields:     fields{transferKeeper: &MockTransferKeeper{}},
			args:       args{caller: senderEvmAddress, callingContract: common.Address{}, input: commonArgs.input, suppliedGas: 1000000, value: nil},
			wantBz:     nil,
			wantErr:    true,
			wantErrMsg: "cannot delegatecall IBC",
		},
		{
			name:   "failed transfer: empty sourcePort",
			fields: fields{transferKeeper: &MockTransferKeeper{}},
			args: args{
				caller:          senderEvmAddress,
				callingContract: senderEvmAddress,
				input: &input{
					receiverAddr:     receiverAddress,
					sourcePort:       "", // empty sourcePort
					sourceChannel:    "channel-0",
					denom:            "denom",
					amount:           big.NewInt(100),
					revisionNumber:   1,
					revisionHeight:   1,
					timeoutTimestamp: 1,
				},
				suppliedGas: uint64(1000000),
				value:       nil,
			},
			wantBz:     nil,
			wantErr:    true,
			wantErrMsg: "port cannot be empty",
		},
		{
			name:   "failed transfer: empty sourceChannel",
			fields: fields{transferKeeper: &MockTransferKeeper{}},
			args: args{
				caller:          senderEvmAddress,
				callingContract: senderEvmAddress,
				input: &input{
					receiverAddr:     receiverAddress,
					sourcePort:       "transfer",
					sourceChannel:    "",
					denom:            "denom",
					amount:           big.NewInt(100),
					revisionNumber:   1,
					revisionHeight:   1,
					timeoutTimestamp: 1,
				},
				suppliedGas: uint64(1000000),
				value:       nil,
			},
			wantBz:     nil,
			wantErr:    true,
			wantErrMsg: "channelID cannot be empty",
		},
		{
			name:   "failed transfer: invalid denom",
			fields: fields{transferKeeper: &MockTransferKeeper{}},
			args: args{
				caller:          senderEvmAddress,
				callingContract: senderEvmAddress,
				input: &input{
					receiverAddr:     receiverAddress,
					sourcePort:       "transfer",
					sourceChannel:    "channel-0",
					denom:            "",
					amount:           big.NewInt(100),
					revisionNumber:   1,
					revisionHeight:   1,
					timeoutTimestamp: 1,
				},
				suppliedGas: uint64(1000000),
				value:       nil,
			},
			wantBz:     nil,
			wantErr:    true,
			wantErrMsg: "invalid denom",
		},
		{
			name:   "failed transfer: invalid receiver address",
			fields: fields{transferKeeper: &MockTransferKeeper{}},
			args: args{
				caller:          senderEvmAddress,
				callingContract: senderEvmAddress,
				input: &input{
					receiverAddr:     "invalid",
					sourcePort:       "transfer",
					sourceChannel:    "channel-0",
					denom:            "",
					amount:           big.NewInt(100),
					revisionNumber:   1,
					revisionHeight:   1,
					timeoutTimestamp: 1,
				},
				suppliedGas: uint64(1000000),
				value:       nil,
			},
			wantBz:     nil,
			wantErr:    true,
			wantErrMsg: "decoding bech32 failed: invalid bech32 string length 7",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testApp := testkeeper.EVMTestApp
			ctx := testApp.NewContext(false, tmtypes.Header{}).WithBlockHeight(2)
			k := &testApp.EvmKeeper
			k.SetAddressMapping(ctx, senderSeiAddress, senderEvmAddress)
			stateDb := state.NewDBImpl(ctx, k, true)
			evm := vm.EVM{
				StateDB:   stateDb,
				TxContext: vm.TxContext{Origin: senderEvmAddress},
			}
			p, _ := ibc.NewPrecompile(tt.fields.transferKeeper, k, nil, nil, nil)
			transfer, err := p.ABI.MethodById(p.TransferID)
			require.Nil(t, err)
			inputs, err := transfer.Inputs.Pack(tt.args.input.receiverAddr,
				tt.args.input.sourcePort, tt.args.input.sourceChannel, tt.args.input.denom, tt.args.input.amount,
				tt.args.input.revisionNumber, tt.args.input.revisionHeight, tt.args.input.timeoutTimestamp)
			require.Nil(t, err)
			gotBz, gotRemainingGas, err := p.RunAndCalculateGas(&evm, tt.args.caller, tt.args.callingContract, append(p.TransferID, inputs...), tt.args.suppliedGas, tt.args.value, nil, false)
			if (err != nil) != tt.wantErr {
				t.Errorf("Run() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil {
				require.Equal(t, tt.wantErrMsg, err.Error())
			}

			if !reflect.DeepEqual(gotBz, tt.wantBz) {
				t.Errorf("Run() gotBz = %v, want %v", gotBz, tt.wantBz)
			}
			if !reflect.DeepEqual(gotRemainingGas, tt.wantRemainingGas) {
				t.Errorf("Run() gotRemainingGas = %v, want %v", gotRemainingGas, tt.wantRemainingGas)
			}
		})
	}
}
