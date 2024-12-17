package confidentialtransfers

import (
	"embed"
	"errors"
	"fmt"
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	pcommon "github.com/sei-protocol/sei-chain/precompiles/common"
	cttypes "github.com/sei-protocol/sei-chain/x/confidentialtransfers/types"
)

const (
	TransferMethod = "transfer"
)

const (
	CtAddress = "0x0000000000000000000000000000000000001010"
)

// Embed abi json file to the executable binary. Needed when importing as dependency.
//
//go:embed abi.json
var f embed.FS

type PrecompileExecutor struct {
	evmKeeper pcommon.EVMKeeper
	ctKeeper  pcommon.ConfidentialTransfersKeeper
	address   common.Address

	TransferID []byte
}

func NewPrecompile(ctkeeper pcommon.ConfidentialTransfersKeeper, evmKeeper pcommon.EVMKeeper) (*pcommon.DynamicGasPrecompile, error) {
	newAbi := pcommon.MustGetABI(f, "abi.json")

	p := &PrecompileExecutor{
		evmKeeper: evmKeeper,
		ctKeeper:  ctkeeper,
		address:   common.HexToAddress(CtAddress),
	}

	for name, m := range newAbi.Methods {
		switch name {
		case TransferMethod:
			p.TransferID = m.ID
		}
	}

	return pcommon.NewDynamicGasPrecompile(newAbi, p, p.address, "confidentialtransfers"), nil
}

func (p PrecompileExecutor) Execute(ctx sdk.Context, method *abi.Method, caller common.Address, callingContract common.Address, args []interface{}, value *big.Int, readOnly bool, evm *vm.EVM, suppliedGas uint64) (ret []byte, remainingGas uint64, err error) {
	if ctx.EVMPrecompileCalledFromDelegateCall() {
		return nil, 0, errors.New("cannot delegatecall ct")
	}
	switch method.Name {
	case TransferMethod:
		if readOnly {
			return nil, 0, errors.New("cannot call ct precompile from staticcall")
		}
		return p.transfer(ctx, method, caller, args, value)
	}
	return
}

func (p PrecompileExecutor) EVMKeeper() pcommon.EVMKeeper {
	return p.evmKeeper
}

func (p PrecompileExecutor) transfer(ctx sdk.Context, method *abi.Method, caller common.Address, args []interface{}, value *big.Int) (ret []byte, remainingGas uint64, rerr error) {
	defer func() {
		if err := recover(); err != nil {
			ret = nil
			remainingGas = 0
			rerr = fmt.Errorf("%s", err)
			return
		}
	}()
	if err := pcommon.ValidateNonPayable(value); err != nil {
		rerr = err
		return
	}

	if err := pcommon.ValidateArgsLength(args, 10); err != nil {
		rerr = err
		return
	}

	fromAddr, err := p.accAddressFromArg(ctx, args[0])
	if err != nil {
		rerr = err
		return
	}

	toAddress, err := p.accAddressFromArg(ctx, args[1])
	if err != nil {
		rerr = err
		return
	}

	denom := args[2].(string)
	if denom == "" {
		rerr = errors.New("invalid denom")
		return
	}

	var fromAmountLo cttypes.Ciphertext
	err = fromAmountLo.Unmarshal(args[3].([]byte))
	if err != nil {
		rerr = err
		return
	}

	var fromAmountHi cttypes.Ciphertext
	err = fromAmountHi.Unmarshal(args[4].([]byte))
	if err != nil {
		rerr = err
		return
	}

	var toAmountLo cttypes.Ciphertext
	err = toAmountLo.Unmarshal(args[5].([]byte))
	if err != nil {
		rerr = err
		return
	}

	var toAmountHi cttypes.Ciphertext
	err = toAmountHi.Unmarshal(args[6].([]byte))
	if err != nil {
		rerr = err
		return
	}

	var remainingBalance cttypes.Ciphertext
	err = remainingBalance.Unmarshal(args[7].([]byte))
	if err != nil {
		rerr = err
		return
	}

	decryptableBalance := args[8].(string)
	if decryptableBalance == "" {
		rerr = errors.New("invalid decryptable balance")
		return
	}

	var transferMessageProofs cttypes.TransferMsgProofs
	err = transferMessageProofs.Unmarshal(args[9].([]byte))
	if err != nil {
		rerr = err
		return
	}

	msg := &cttypes.MsgTransfer{
		FromAddress:        fromAddr.String(),
		ToAddress:          toAddress.String(),
		Denom:              denom,
		FromAmountLo:       &fromAmountLo,
		FromAmountHi:       &fromAmountHi,
		ToAmountLo:         &toAmountLo,
		ToAmountHi:         &toAmountHi,
		RemainingBalance:   &remainingBalance,
		DecryptableBalance: decryptableBalance,
		Proofs:             &transferMessageProofs,
	}

	err = msg.ValidateBasic()
	if err != nil {
		rerr = err
		return
	}
	_, err = p.ctKeeper.Transfer(sdk.WrapSDKContext(ctx), msg)
	if err != nil {
		rerr = err
		return
	}
	ret, rerr = method.Outputs.Pack(true)
	remainingGas = pcommon.GetRemainingGas(ctx, p.evmKeeper)
	return
}

func (p PrecompileExecutor) accAddressFromArg(ctx sdk.Context, arg interface{}) (sdk.AccAddress, error) {
	addr := arg.(common.Address)
	if addr == (common.Address{}) {
		return nil, errors.New("invalid addr")
	}
	seiAddr, associated := p.evmKeeper.GetSeiAddress(ctx, addr)
	if !associated {
		return nil, errors.New("cannot use an unassociated address as withdraw address")
	}
	return seiAddr, nil
}
