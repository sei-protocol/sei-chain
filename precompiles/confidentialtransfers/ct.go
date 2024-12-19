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
	TransferMethod             = "transfer"
	TransferWithAuditorsMethod = "transferWithAuditors"
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

	TransferID             []byte
	TransferWithAuditorsID []byte
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
		case TransferWithAuditorsMethod:
			p.TransferWithAuditorsID = m.ID
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
	case TransferWithAuditorsMethod:
		if readOnly {
			return nil, 0, errors.New("cannot call ct precompile from staticcall")
		}
		return p.transferWithAuditors(ctx, method, caller, args, value)
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

	msg, err := p.getTransferMessageFromArgs(ctx, args)
	if err != nil {
		rerr = err
		return
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

func (p PrecompileExecutor) transferWithAuditors(ctx sdk.Context, method *abi.Method, caller common.Address, args []interface{}, value *big.Int) (ret []byte, remainingGas uint64, rerr error) {
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

	if err := pcommon.ValidateArgsLength(args, 11); err != nil {
		rerr = err
		return
	}

	msg, err := p.getTransferMessageFromArgs(ctx, args)
	if err != nil {
		rerr = err
		return
	}

	res := args[10].([]struct {
		AuditorAddress                common.Address `json:"auditorAddress"`
		EncryptedTransferAmountLo     []byte         `json:"encryptedTransferAmountLo"`
		EncryptedTransferAmountHi     []byte         `json:"encryptedTransferAmountHi"`
		TransferAmountLoValidityProof []byte         `json:"transferAmountLoValidityProof"`
		TransferAmountHiValidityProof []byte         `json:"transferAmountHiValidityProof"`
		TransferAmountLoEqualityProof []byte         `json:"transferAmountLoEqualityProof"`
		TransferAmountHiEqualityProof []byte         `json:"transferAmountHiEqualityProof"`
	})

	var auditors []*cttypes.Auditor
	for _, auditor := range res {
		auditorAddr, err := p.accAddressFromArg(ctx, auditor.AuditorAddress)
		if err != nil {
			rerr = err
			return
		}

		var encryptedTransferAmountLo cttypes.Ciphertext
		err = encryptedTransferAmountLo.Unmarshal(auditor.EncryptedTransferAmountLo)
		if err != nil {
			rerr = err
			return
		}

		var encryptedTransferAmountHi cttypes.Ciphertext
		err = encryptedTransferAmountHi.Unmarshal(auditor.EncryptedTransferAmountHi)
		if err != nil {
			rerr = err
			return
		}

		var transferAmountLoValidityProof cttypes.CiphertextValidityProof
		err = transferAmountLoValidityProof.Unmarshal(auditor.TransferAmountLoValidityProof)
		if err != nil {
			rerr = err
			return
		}

		var transferAmountHiValidityProof cttypes.CiphertextValidityProof
		err = transferAmountHiValidityProof.Unmarshal(auditor.TransferAmountHiValidityProof)
		if err != nil {
			rerr = err
			return
		}

		var transferAmountLoEqualityProof cttypes.CiphertextCiphertextEqualityProof
		err = transferAmountLoEqualityProof.Unmarshal(auditor.TransferAmountLoEqualityProof)
		if err != nil {
			rerr = err
			return
		}

		var transferAmountHiEqualityProof cttypes.CiphertextCiphertextEqualityProof
		err = transferAmountHiEqualityProof.Unmarshal(auditor.TransferAmountHiEqualityProof)
		if err != nil {
			rerr = err
			return
		}

		a := &cttypes.Auditor{
			AuditorAddress:                auditorAddr.String(),
			EncryptedTransferAmountLo:     &encryptedTransferAmountLo,
			EncryptedTransferAmountHi:     &encryptedTransferAmountHi,
			TransferAmountLoValidityProof: &transferAmountLoValidityProof,
			TransferAmountHiValidityProof: &transferAmountHiValidityProof,
			TransferAmountLoEqualityProof: &transferAmountLoEqualityProof,
			TransferAmountHiEqualityProof: &transferAmountHiEqualityProof,
		}
		auditors = append(auditors, a)
	}

	msg.Auditors = auditors

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

func (p PrecompileExecutor) getTransferMessageFromArgs(ctx sdk.Context, args []interface{}) (*cttypes.MsgTransfer, error) {
	fromAddr, err := p.accAddressFromArg(ctx, args[0])
	if err != nil {
		return nil, err
	}

	toAddress, err := p.accAddressFromArg(ctx, args[1])
	if err != nil {
		return nil, err
	}

	denom := args[2].(string)
	if denom == "" {
		return nil, errors.New("invalid denom")
	}

	var fromAmountLo cttypes.Ciphertext
	err = fromAmountLo.Unmarshal(args[3].([]byte))
	if err != nil {
		return nil, err
	}

	var fromAmountHi cttypes.Ciphertext
	err = fromAmountHi.Unmarshal(args[4].([]byte))
	if err != nil {
		return nil, err
	}

	var toAmountLo cttypes.Ciphertext
	err = toAmountLo.Unmarshal(args[5].([]byte))
	if err != nil {
		return nil, err
	}

	var toAmountHi cttypes.Ciphertext
	err = toAmountHi.Unmarshal(args[6].([]byte))
	if err != nil {
		return nil, err
	}

	var remainingBalance cttypes.Ciphertext
	err = remainingBalance.Unmarshal(args[7].([]byte))
	if err != nil {
		return nil, err
	}

	decryptableBalance := args[8].(string)
	if decryptableBalance == "" {
		return nil, errors.New("invalid decryptable balance")
	}

	var transferMessageProofs cttypes.TransferMsgProofs
	err = transferMessageProofs.Unmarshal(args[9].([]byte))
	if err != nil {
		return nil, err
	}

	return &cttypes.MsgTransfer{
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
	}, nil
}

func (p PrecompileExecutor) accAddressFromArg(ctx sdk.Context, arg interface{}) (sdk.AccAddress, error) {
	addr := arg.(common.Address)
	if addr == (common.Address{}) {
		return nil, errors.New("invalid addr")
	}
	seiAddr, associated := p.evmKeeper.GetSeiAddress(ctx, addr)
	if !associated {
		return nil, errors.New("cannot use an unassociated address as transfer address")
	}
	return seiAddr, nil
}
