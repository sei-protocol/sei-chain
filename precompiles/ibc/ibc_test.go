package ibc_test

import (
	"errors"
	"github.com/cosmos/ibc-go/v3/modules/core/exported"
	"math/big"
	"reflect"
	"testing"
	"time"

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

func TestTransferWithDefaultTimeoutPrecompile_Run(t *testing.T) {
	senderSeiAddress, senderEvmAddress := testkeeper.MockAddressPair()
	receiverAddress := "cosmos1yykwxjzr2tv4mhx5tsf8090sdg96f2ax8fydk2"

	type fields struct {
		transferKeeper   pcommon.TransferKeeper
		clientKeeper     pcommon.ClientKeeper
		connectionKeeper pcommon.ConnectionKeeper
		channelKeeper    pcommon.ChannelKeeper
	}

	type input struct {
		receiverAddr  string
		sourcePort    string
		sourceChannel string
		denom         string
		amount        *big.Int
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
			receiverAddr:  receiverAddress,
			sourcePort:    "transfer",
			sourceChannel: "channel-0",
			denom:         "denom",
			amount:        big.NewInt(100),
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
					receiverAddr:  receiverAddress,
					sourcePort:    "", // empty sourcePort
					sourceChannel: "channel-0",
					denom:         "denom",
					amount:        big.NewInt(100),
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
					receiverAddr:  receiverAddress,
					sourcePort:    "transfer",
					sourceChannel: "",
					denom:         "denom",
					amount:        big.NewInt(100),
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
					receiverAddr:  receiverAddress,
					sourcePort:    "transfer",
					sourceChannel: "channel-0",
					denom:         "",
					amount:        big.NewInt(100),
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
					receiverAddr:  "invalid",
					sourcePort:    "transfer",
					sourceChannel: "channel-0",
					denom:         "",
					amount:        big.NewInt(100),
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

			p, _ := ibc.NewPrecompile(tt.fields.transferKeeper,
				k, tt.fields.clientKeeper,
				tt.fields.connectionKeeper,
				tt.fields.channelKeeper)
			transfer, err := p.ABI.MethodById(p.TransferWithDefaultTimeoutID)
			require.Nil(t, err)
			inputs, err := transfer.Inputs.Pack(tt.args.input.receiverAddr,
				tt.args.input.sourcePort, tt.args.input.sourceChannel, tt.args.input.denom, tt.args.input.amount)
			require.Nil(t, err)
			gotBz, gotRemainingGas, err := p.RunAndCalculateGas(&evm,
				tt.args.caller,
				tt.args.callingContract,
				append(p.TransferWithDefaultTimeoutID, inputs...),
				tt.args.suppliedGas,
				tt.args.value,
				nil,
				false)
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

func TestPrecompile_GetAdjustedHeight(t *testing.T) {
	type args struct {
		latestConsensusHeight clienttypes.Height
	}
	tests := []struct {
		name    string
		args    args
		want    clienttypes.Height
		wantErr bool
	}{
		{
			name: "height is adjusted with defaults",
			args: args{
				latestConsensusHeight: clienttypes.NewHeight(2, 3),
			},
			want: clienttypes.Height{
				RevisionNumber: 2,
				RevisionHeight: 1003,
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ibc.GetAdjustedHeight(tt.args.latestConsensusHeight)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetAdjustedHeight() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GetAdjustedHeight() got = %v, want %v", got, tt.want)
			}
		})
	}
}

type MockClientKeeper struct {
	consensusState       *MockConsensusState
	returnConsensusState bool
}

func (ck *MockClientKeeper) GetClientState(ctx sdk.Context, clientID string) (exported.ClientState, bool) {
	return nil, false
}

func (ck *MockClientKeeper) GetClientConsensusState(ctx sdk.Context, clientID string, height exported.Height) (exported.ConsensusState, bool) {
	return ck.consensusState, ck.returnConsensusState
}

type MockConsensusState struct {
	timestamp uint64
}

func (m *MockConsensusState) Reset() {
	panic("implement me")
}

func (m *MockConsensusState) String() string {
	panic("implement me")
}

func (m *MockConsensusState) ProtoMessage() {
	panic("implement me")
}

func (m *MockConsensusState) ClientType() string {
	return "mock"
}

func (m *MockConsensusState) GetRoot() exported.Root {
	return nil
}

func (m *MockConsensusState) GetTimestamp() uint64 {
	return m.timestamp
}

func (m *MockConsensusState) ValidateBasic() error {
	return nil
}

func TestPrecompile_GetAdjustedTimestamp(t *testing.T) {
	type fields struct {
		transferKeeper   pcommon.TransferKeeper
		evmKeeper        pcommon.EVMKeeper
		clientKeeper     pcommon.ClientKeeper
		connectionKeeper pcommon.ConnectionKeeper
		channelKeeper    pcommon.ChannelKeeper
	}
	type args struct {
		ctx      sdk.Context
		clientId string
		height   clienttypes.Height
	}
	timestampSeconds := 1714680155
	ctx := sdk.Context{}
	tests := []struct {
		name    string
		fields  fields
		args    args
		want    uint64
		wantErr bool
	}{
		{
			name: "if consensus timestamp is less than the given time, return the given time adjusted with default",
			fields: fields{
				clientKeeper: &MockClientKeeper{
					consensusState: &MockConsensusState{
						timestamp: uint64(timestampSeconds - 1),
					},
					returnConsensusState: true,
				},
			},
			args: args{
				ctx: ctx.WithBlockTime(time.Unix(int64(timestampSeconds), 0)),
			},
			want:    uint64(timestampSeconds)*1_000_000_000 + uint64((time.Duration(10) * time.Minute).Nanoseconds()),
			wantErr: false,
		},
		{
			name: "if consensus state is not found, return the given time adjusted with default",
			fields: fields{
				clientKeeper: &MockClientKeeper{
					returnConsensusState: false,
				},
			},
			args: args{
				ctx: ctx.WithBlockTime(time.Unix(int64(timestampSeconds), 0)),
			},
			want:    uint64(timestampSeconds)*1_000_000_000 + uint64((time.Duration(10) * time.Minute).Nanoseconds()),
			wantErr: false,
		},
		{
			name: "if time from local clock can not be retrieved, return error",
			fields: fields{
				clientKeeper: &MockClientKeeper{
					returnConsensusState: false,
				},
			},
			args: args{
				ctx: ctx.WithBlockTime(time.Unix(int64(0), 0)),
			},
			wantErr: true,
		},
		{
			name: "if consensus timestamp is > than the given time, return the consensus time adjusted with default",
			fields: fields{
				clientKeeper: &MockClientKeeper{
					consensusState: &MockConsensusState{
						timestamp: uint64(timestampSeconds+1) * 1_000_000_000,
					},
					returnConsensusState: true,
				},
			},
			args: args{
				ctx: ctx.WithBlockTime(time.Unix(int64(timestampSeconds), 0)),
			},
			want:    uint64(timestampSeconds+1)*1_000_000_000 + uint64((time.Duration(10) * time.Minute).Nanoseconds()),
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, _ := ibc.NewPrecompile(tt.fields.transferKeeper, tt.fields.evmKeeper, tt.fields.clientKeeper, tt.fields.connectionKeeper, tt.fields.channelKeeper)
			got, err := p.GetAdjustedTimestamp(tt.args.ctx, tt.args.clientId, tt.args.height)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetAdjustedTimestamp() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("GetAdjustedTimestamp() got = %v, want %v", got, tt.want)
			}
		})
	}
}
