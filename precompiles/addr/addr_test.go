package addr_test

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"

	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/sei-protocol/sei-chain/precompiles/addr"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/stretchr/testify/require"
	tmtypes "github.com/tendermint/tendermint/proto/tendermint/types"
)

func TestAssociateWithGas(t *testing.T) {
	testApp := testkeeper.EVMTestApp
	ctx := testApp.NewContext(false, tmtypes.Header{}).WithBlockHeight(2)
	k := &testApp.EvmKeeper

	pre, _ := addr.NewPrecompile(k, k.BankKeeper(), k.AccountKeeper())
	associateWithGas, err := pre.ABI.MethodById(pre.GetExecutor().(*addr.PrecompileExecutor).AssociateWithGasID)

	// Target refers to the address that the caller is trying to associate.
	targetPrivKey := testkeeper.MockPrivateKey()
	targetPrivHex := hex.EncodeToString(targetPrivKey.Bytes())
	targetSeiAddress, targetEvmAddress := testkeeper.PrivateKeyToAddresses(targetPrivKey)
	targetKey, _ := crypto.HexToECDSA(targetPrivHex)

	// Create the inputs
	emptyData := make([]byte, 32)
	prefixedMessage := fmt.Sprintf("\x19Ethereum Signed Message:\n%d", len(emptyData)) + string(emptyData)
	hash := crypto.Keccak256Hash([]byte(prefixedMessage))
	sig, err := crypto.Sign(hash.Bytes(), targetKey)
	require.Nil(t, err)

	r := fmt.Sprintf("0x%v", new(big.Int).SetBytes(sig[:32]).Text(16))
	s := fmt.Sprintf("0x%v", new(big.Int).SetBytes(sig[32:64]).Text(16))
	v := fmt.Sprintf("0x%v", new(big.Int).SetBytes([]byte{sig[64]}).Text(16))

	// Caller refers to the party calling the precompile.
	callerPrivKey := testkeeper.MockPrivateKey()
	callerSeiAddress, callerEvmAddress := testkeeper.PrivateKeyToAddresses(callerPrivKey)
	callerPrivHex := hex.EncodeToString(callerPrivKey.Bytes())
	callerKey, _ := crypto.HexToECDSA(callerPrivHex)

	// Associate these addresses, so we can use them to test the case where addresses are already associated association.
	k.SetAddressMapping(ctx, callerSeiAddress, callerEvmAddress)
	callerSig, err := crypto.Sign(hash.Bytes(), callerKey)
	callerR := fmt.Sprintf("0x%v", new(big.Int).SetBytes(callerSig[:32]).Text(16))
	callerS := fmt.Sprintf("0x%v", new(big.Int).SetBytes(callerSig[32:64]).Text(16))
	callerV := fmt.Sprintf("0x%v", new(big.Int).SetBytes([]byte{callerSig[64]}).Text(16))

	happyPathOutput, _ := associateWithGas.Outputs.Pack(addr.AddrPair{SeiAddr: targetSeiAddress.String(), EvmAddr: targetEvmAddress})

	type args struct {
		evm    *vm.EVM
		caller common.Address
		v      string
		r      string
		s      string
		msg    string
		value  *big.Int
	}
	tests := []struct {
		name       string
		args       args
		wantRet    []byte
		wantErr    bool
		wantErrMsg string
		wrongRet   bool
	}{
		{
			name: "fails if payable",
			args: args{
				evm: &vm.EVM{
					StateDB:   state.NewDBImpl(ctx, k, true),
					TxContext: vm.TxContext{Origin: callerEvmAddress},
				},
				caller: callerEvmAddress,
				v:      v,
				r:      r,
				s:      s,
				msg:    prefixedMessage,
				value:  big.NewInt(10),
			},
			wantRet:    nil,
			wantErr:    true,
			wantErrMsg: "sending funds to a non-payable function",
			wrongRet:   false,
		},
		{
			name: "fails if input is not hex",
			args: args{
				evm: &vm.EVM{
					StateDB:   state.NewDBImpl(ctx, k, true),
					TxContext: vm.TxContext{Origin: callerEvmAddress},
				},
				caller: callerEvmAddress,
				v:      "nothex",
				r:      r,
				s:      s,
				msg:    prefixedMessage,
				value:  big.NewInt(0),
			},
			wantRet:    nil,
			wantErr:    true,
			wantErrMsg: "encoding/hex: invalid byte: U+006E 'n'",
			wrongRet:   false,
		},
		{
			name: "fails if addresses are already associated",
			args: args{
				evm: &vm.EVM{
					StateDB:   state.NewDBImpl(ctx, k, true),
					TxContext: vm.TxContext{Origin: callerEvmAddress},
				},
				caller: callerEvmAddress,
				v:      callerV,
				r:      callerR,
				s:      callerS,
				msg:    prefixedMessage,
				value:  big.NewInt(0),
			},
			wantRet:    nil,
			wantErr:    true,
			wantErrMsg: fmt.Sprintf("address %s is already associated with evm address %s", callerSeiAddress, callerEvmAddress),
		},
		{
			name: "associates wrong address if invalid signature (different values)",
			args: args{
				evm: &vm.EVM{
					StateDB:   state.NewDBImpl(ctx, k, true),
					TxContext: vm.TxContext{Origin: callerEvmAddress},
				},
				caller: callerEvmAddress,
				v:      v,
				r:      r,
				s:      r, // Pass in r instead of s here for invalid value
				msg:    prefixedMessage,
				value:  big.NewInt(0),
			},
			wantRet:    happyPathOutput,
			wantErr:    false,
			wantErrMsg: "",
			wrongRet:   true,
		},
		{
			name: "associates wrong address if invalid signature (different message)",
			args: args{
				evm: &vm.EVM{
					StateDB:   state.NewDBImpl(ctx, k, true),
					TxContext: vm.TxContext{Origin: callerEvmAddress},
				},
				caller: callerEvmAddress,
				v:      v,
				r:      r,
				s:      s, // Pass in r instead of s here for invalid value
				msg:    "Not the signed message",
				value:  big.NewInt(0),
			},
			wantRet:    happyPathOutput,
			wantErr:    false,
			wantErrMsg: "",
			wrongRet:   true,
		},
		{
			name: "happy path",
			args: args{
				evm: &vm.EVM{
					StateDB:   state.NewDBImpl(ctx, k, true),
					TxContext: vm.TxContext{Origin: callerEvmAddress},
				},
				caller: callerEvmAddress,
				v:      v,
				r:      r,
				s:      s,
				msg:    prefixedMessage,
				value:  big.NewInt(0),
			},
			wantRet: happyPathOutput,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create the precompile and inputs
			p, _ := addr.NewPrecompile(k, k.BankKeeper(), k.AccountKeeper())
			require.Nil(t, err)
			inputs, err := associateWithGas.Inputs.Pack(tt.args.v, tt.args.r, tt.args.s, tt.args.msg)
			require.Nil(t, err)

			// Make the call to associateWithGas.
			ret, err := p.Run(tt.args.evm, tt.args.caller, tt.args.caller, append(p.GetExecutor().(*addr.PrecompileExecutor).AssociateWithGasID, inputs...), tt.args.value, false)
			if (err != nil) != tt.wantErr {
				t.Errorf("Run() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil {
				require.Equal(t, vm.ErrExecutionReverted, err)
				require.Equal(t, tt.wantErrMsg, string(ret))
			} else if tt.wrongRet {
				// tt.wrongRet is set if we expect a return value that's different from the happy path. This means that the wrong addresses were associated.
				require.NotEqual(t, tt.wantRet, ret)
			} else {
				require.Equal(t, tt.wantRet, ret)
			}
		})
	}
}
