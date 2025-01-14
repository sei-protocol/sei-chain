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
	InitializeAccountMethod    = "initializeAccount"
	DepositMethod              = "deposit"
	ApplyPendingBalanceMethod  = "applyPendingBalance"
	TransferMethod             = "transfer"
	TransferWithAuditorsMethod = "transferWithAuditors"
	WithdrawMethod             = "withdraw"
	CloseAccountMethod         = "closeAccount"
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

	InitializeAccountID    []byte
	DepositID              []byte
	ApplyPendingBalanceID  []byte
	TransferID             []byte
	TransferWithAuditorsID []byte
	WithdrawID             []byte
	CloseAccountID         []byte
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
		case DepositMethod:
			p.DepositID = m.ID
		case InitializeAccountMethod:
			p.InitializeAccountID = m.ID
		case ApplyPendingBalanceMethod:
			p.ApplyPendingBalanceID = m.ID
		case TransferMethod:
			p.TransferID = m.ID
		case TransferWithAuditorsMethod:
			p.TransferWithAuditorsID = m.ID
		case WithdrawMethod:
			p.WithdrawID = m.ID
		case CloseAccountMethod:
			p.CloseAccountID = m.ID
		}
	}

	return pcommon.NewDynamicGasPrecompile(newAbi, p, p.address, "confidentialtransfers"), nil
}

func (p PrecompileExecutor) Execute(ctx sdk.Context, method *abi.Method, caller common.Address, callingContract common.Address, args []interface{}, value *big.Int, readOnly bool, evm *vm.EVM, suppliedGas uint64) (ret []byte, remainingGas uint64, err error) {
	if ctx.EVMPrecompileCalledFromDelegateCall() {
		return nil, 0, errors.New("cannot delegatecall ct")
	}
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, 0, err
	}
	switch method.Name {
	case ApplyPendingBalanceMethod:
		if readOnly {
			return nil, 0, errors.New("cannot call ct precompile from staticcall")
		}
		return p.applyPendingBalance(ctx, method, caller, args)
	case DepositMethod:
		if readOnly {
			return nil, 0, errors.New("cannot call ct precompile from staticcall")
		}
		return p.deposit(ctx, method, caller, args)
	case InitializeAccountMethod:
		if readOnly {
			return nil, 0, errors.New("cannot call ct precompile from staticcall")
		}
		return p.initializeAccount(ctx, method, caller, args)
	case TransferMethod:
		if readOnly {
			return nil, 0, errors.New("cannot call ct precompile from staticcall")
		}
		return p.transfer(ctx, method, caller, args)
	case TransferWithAuditorsMethod:
		if readOnly {
			return nil, 0, errors.New("cannot call ct precompile from staticcall")
		}
		return p.transferWithAuditors(ctx, method, caller, args)
	case WithdrawMethod:
		if readOnly {
			return nil, 0, errors.New("cannot call ct precompile from staticcall")
		}
		return p.withdraw(ctx, method, caller, args)
	case CloseAccountMethod:
		if readOnly {
			return nil, 0, errors.New("cannot call ct precompile from staticcall")
		}
		return p.closeAccount(ctx, method, caller, args)
	}
	return
}

func (p PrecompileExecutor) EVMKeeper() pcommon.EVMKeeper {
	return p.evmKeeper
}

func (p PrecompileExecutor) transfer(ctx sdk.Context, method *abi.Method, caller common.Address, args []interface{}) (ret []byte, remainingGas uint64, rerr error) {
	defer func() {
		if err := recover(); err != nil {
			ret = nil
			remainingGas = 0
			rerr = fmt.Errorf("%s", err)
			return
		}
	}()

	if err := pcommon.ValidateArgsLength(args, 9); err != nil {
		rerr = err
		return
	}

	msg, err := p.getTransferMessageFromArgs(ctx, caller, args)
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

func (p PrecompileExecutor) transferWithAuditors(ctx sdk.Context, method *abi.Method, caller common.Address, args []interface{}) (ret []byte, remainingGas uint64, rerr error) {
	defer func() {
		if err := recover(); err != nil {
			ret = nil
			remainingGas = 0
			rerr = fmt.Errorf("%s", err)
			return
		}
	}()

	if err := pcommon.ValidateArgsLength(args, 10); err != nil {
		rerr = err
		return
	}

	msg, err := p.getTransferMessageFromArgs(ctx, caller, args)
	if err != nil {
		rerr = err
		return
	}

	msg.Auditors, err = p.getAuditorsFromArg(ctx, args[9])
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

func (p PrecompileExecutor) getTransferMessageFromArgs(ctx sdk.Context, caller common.Address, args []interface{}) (*cttypes.MsgTransfer, error) {
	fromAddr, _, err := p.getAssociatedAddressesByEVMAddress(ctx, caller)
	if err != nil {
		return nil, err
	}

	toAddrString, ok := (args[0]).(string)
	if !ok || toAddrString == "" {
		return nil, errors.New("invalid to addr")
	}

	toAddress, err := p.getValidSeiAddressFromString(ctx, toAddrString)
	if err != nil {
		return nil, err
	}

	denom, ok := args[1].(string)
	if !ok || denom == "" {
		return nil, errors.New("invalid denom")
	}

	var fromAmountLo cttypes.Ciphertext
	err = fromAmountLo.Unmarshal(args[2].([]byte))
	if err != nil {
		return nil, err
	}

	var fromAmountHi cttypes.Ciphertext
	err = fromAmountHi.Unmarshal(args[3].([]byte))
	if err != nil {
		return nil, err
	}

	var toAmountLo cttypes.Ciphertext
	err = toAmountLo.Unmarshal(args[4].([]byte))
	if err != nil {
		return nil, err
	}

	var toAmountHi cttypes.Ciphertext
	err = toAmountHi.Unmarshal(args[5].([]byte))
	if err != nil {
		return nil, err
	}

	var remainingBalance cttypes.Ciphertext
	err = remainingBalance.Unmarshal(args[6].([]byte))
	if err != nil {
		return nil, err
	}

	decryptableBalance, ok := args[7].(string)
	if !ok || decryptableBalance == "" {
		return nil, errors.New("invalid decryptable balance")
	}

	var transferMessageProofs cttypes.TransferMsgProofs
	err = transferMessageProofs.Unmarshal(args[8].([]byte))
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

// getValidAddressesFromString returns the associated Sei and EVM addresses given an EVM address
// It returns an error if the Sei or EVM address is not associated
func (p PrecompileExecutor) getValidAddressesFromString(ctx sdk.Context, addr string) (sdk.AccAddress, common.Address, error) {
	if common.IsHexAddress(addr) {
		return p.getAssociatedAddressesByEVMAddress(ctx, common.HexToAddress(addr))
	}
	return p.getAssociatedAddressesBySeiAddress(ctx, addr)
}

// getValidSeiAddressFromString returns the validated Sei address given an (EVM or native Sei) address string
// This method is for the case when we need to get the Sei address, but do not require it to be associated with an EVM
// address (unless EVM address is provided as an argument)
func (p PrecompileExecutor) getValidSeiAddressFromString(ctx sdk.Context, addr string) (sdk.AccAddress, error) {
	if common.IsHexAddress(addr) {
		seiAddr, _, err := p.getAssociatedAddressesByEVMAddress(ctx, common.HexToAddress(addr))
		return seiAddr, err
	}
	seiAddr, err := sdk.AccAddressFromBech32(addr)
	if err != nil {
		return nil, fmt.Errorf("invalid address %s: %w", addr, err)
	}
	return seiAddr, nil
}

// getAssociatedAddressesByEVMAddress returns the associated Sei and EVM addresses given an EVM address
// It returns an error if the Sei address is not associated
func (p PrecompileExecutor) getAssociatedAddressesByEVMAddress(ctx sdk.Context, evmAddr common.Address) (sdk.AccAddress, common.Address, error) {
	seiAddr, associated := p.evmKeeper.GetSeiAddress(ctx, evmAddr)
	if !associated {
		return nil, common.Address{}, fmt.Errorf("address %s is not associated", evmAddr)
	}
	return seiAddr, evmAddr, nil
}

// getAssociatedAddressesBySeiAddress returns the associated Sei and EVM addresses given a Sei address
// It returns an error if the address is not associated or if the address is invalid
// Situation where EVM address is not associated with Sei address is unlikely to happen in this layer, since when EVM
// transaction is sent, the EVM address should be associated with SEI address in ante handler. We add the check anyway
// for safety and in case we hit this edge case it's clear what the error is.
func (p PrecompileExecutor) getAssociatedAddressesBySeiAddress(ctx sdk.Context, addr string) (sdk.AccAddress, common.Address, error) {
	seiAddr, err := sdk.AccAddressFromBech32(addr)
	if err != nil {
		return nil, common.Address{}, fmt.Errorf("invalid address %s: %w", addr, err)
	}
	evmAddr, associated := p.evmKeeper.GetEVMAddress(ctx, seiAddr)
	if !associated {
		return nil, common.Address{}, fmt.Errorf("address %s is not associated", addr)
	}
	return seiAddr, evmAddr, nil
}

func (p PrecompileExecutor) getAuditorsFromArg(ctx sdk.Context, arg interface{}) (auditorsArray []*cttypes.Auditor, rerr error) {
	defer func() {
		if err := recover(); err != nil {
			auditorsArray = nil
			rerr = fmt.Errorf("error parsing auditors array: %s", err)
			return
		}
	}()
	// we need to define an anonymous struct similar to types.CtAuditor because the ABI returns an anonymous struct
	evmAuditors := arg.([]struct {
		AuditorAddress                string `json:"auditorAddress"`
		EncryptedTransferAmountLo     []byte `json:"encryptedTransferAmountLo"`
		EncryptedTransferAmountHi     []byte `json:"encryptedTransferAmountHi"`
		TransferAmountLoValidityProof []byte `json:"transferAmountLoValidityProof"`
		TransferAmountHiValidityProof []byte `json:"transferAmountHiValidityProof"`
		TransferAmountLoEqualityProof []byte `json:"transferAmountLoEqualityProof"`
		TransferAmountHiEqualityProof []byte `json:"transferAmountHiEqualityProof"`
	})

	if len(evmAuditors) == 0 {
		return nil, errors.New("auditors array cannot be empty")
	}

	auditors := make([]*cttypes.Auditor, 0)
	for _, auditor := range evmAuditors {
		auditorAddr, err := p.getValidSeiAddressFromString(ctx, auditor.AuditorAddress)
		if err != nil {
			return nil, err
		}

		var encryptedTransferAmountLo cttypes.Ciphertext
		err = encryptedTransferAmountLo.Unmarshal(auditor.EncryptedTransferAmountLo)
		if err != nil {
			return nil, err
		}

		var encryptedTransferAmountHi cttypes.Ciphertext
		err = encryptedTransferAmountHi.Unmarshal(auditor.EncryptedTransferAmountHi)
		if err != nil {
			return nil, err
		}

		var transferAmountLoValidityProof cttypes.CiphertextValidityProof
		err = transferAmountLoValidityProof.Unmarshal(auditor.TransferAmountLoValidityProof)
		if err != nil {
			return nil, err
		}
		var transferAmountHiValidityProof cttypes.CiphertextValidityProof
		err = transferAmountHiValidityProof.Unmarshal(auditor.TransferAmountHiValidityProof)
		if err != nil {
			return nil, err
		}

		var transferAmountLoEqualityProof cttypes.CiphertextCiphertextEqualityProof
		err = transferAmountLoEqualityProof.Unmarshal(auditor.TransferAmountLoEqualityProof)
		if err != nil {
			return nil, err
		}

		var transferAmountHiEqualityProof cttypes.CiphertextCiphertextEqualityProof
		err = transferAmountHiEqualityProof.Unmarshal(auditor.TransferAmountHiEqualityProof)
		if err != nil {
			return nil, err
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
	return auditors, nil
}

func (p PrecompileExecutor) initializeAccount(ctx sdk.Context, method *abi.Method, caller common.Address, args []interface{}) (ret []byte, remainingGas uint64, rerr error) {
	defer func() {
		if err := recover(); err != nil {
			ret = nil
			remainingGas = 0
			rerr = fmt.Errorf("%s", err)
			return
		}
	}()

	if err := pcommon.ValidateArgsLength(args, 8); err != nil {
		rerr = err
		return
	}

	seiAddr, evmAddr, err := p.getValidAddressesFromString(ctx, args[0].(string))
	if err != nil {
		rerr = err
		return
	}

	if evmAddr != caller {
		rerr = errors.New("caller is not the same as the user address")
		return
	}

	denom := args[1].(string)
	if denom == "" {
		rerr = errors.New("invalid denom")
		return
	}

	publicKey, ok := args[2].([]byte)
	if !ok {
		rerr = errors.New("invalid public key")
		return
	}

	decryptableBalance := args[3].(string)
	if decryptableBalance == "" {
		rerr = errors.New("invalid decryptable balance")
		return
	}

	var pendingBalanceLo cttypes.Ciphertext
	err = pendingBalanceLo.Unmarshal(args[4].([]byte))
	if err != nil {
		rerr = err
		return
	}

	var pendingBalanceHi cttypes.Ciphertext
	err = pendingBalanceHi.Unmarshal(args[5].([]byte))
	if err != nil {
		rerr = err
		return
	}

	var availableBalance cttypes.Ciphertext
	err = availableBalance.Unmarshal(args[6].([]byte))
	if err != nil {
		rerr = err
		return
	}

	var initializeAccountProofs cttypes.InitializeAccountMsgProofs
	err = initializeAccountProofs.Unmarshal(args[7].([]byte))
	if err != nil {
		rerr = err
		return
	}

	msg := &cttypes.MsgInitializeAccount{
		FromAddress:        seiAddr.String(),
		Denom:              denom,
		PublicKey:          publicKey,
		DecryptableBalance: decryptableBalance,
		PendingBalanceLo:   &pendingBalanceLo,
		PendingBalanceHi:   &pendingBalanceHi,
		AvailableBalance:   &availableBalance,
		Proofs:             &initializeAccountProofs,
	}

	_, err = p.ctKeeper.InitializeAccount(sdk.WrapSDKContext(ctx), msg)
	if err != nil {
		rerr = err
		return
	}
	ret, rerr = method.Outputs.Pack(true)
	remainingGas = pcommon.GetRemainingGas(ctx, p.evmKeeper)
	return
}

func (p PrecompileExecutor) deposit(ctx sdk.Context, method *abi.Method, caller common.Address, args []interface{}) (ret []byte, remainingGas uint64, rerr error) {
	defer func() {
		if err := recover(); err != nil {
			ret = nil
			remainingGas = 0
			rerr = fmt.Errorf("%s", err)
			return
		}
	}()

	if err := pcommon.ValidateArgsLength(args, 2); err != nil {
		rerr = err
		return
	}

	seiAddr, _, err := p.getAssociatedAddressesByEVMAddress(ctx, caller)
	if err != nil {
		rerr = err
		return
	}

	denom := args[0].(string)
	if denom == "" {
		rerr = errors.New("invalid denom")
		return
	}

	// for usei denom amount should be treated as 6 decimal instead of 19 decimal
	amount, ok := args[1].(uint64)
	if !ok {
		rerr = errors.New("invalid amount")
		return
	}

	msg := &cttypes.MsgDeposit{
		FromAddress: seiAddr.String(),
		Denom:       denom,
		Amount:      amount,
	}

	_, err = p.ctKeeper.Deposit(sdk.WrapSDKContext(ctx), msg)
	if err != nil {
		rerr = err
		return
	}
	ret, rerr = method.Outputs.Pack(true)
	remainingGas = pcommon.GetRemainingGas(ctx, p.evmKeeper)
	return
}

func (p PrecompileExecutor) applyPendingBalance(ctx sdk.Context, method *abi.Method, caller common.Address, args []interface{}) (ret []byte, remainingGas uint64, rerr error) {
	defer func() {
		if err := recover(); err != nil {
			ret = nil
			remainingGas = 0
			rerr = fmt.Errorf("%s", err)
			return
		}
	}()

	if err := pcommon.ValidateArgsLength(args, 4); err != nil {
		rerr = err
		return
	}

	fromAddr, _, err := p.getAssociatedAddressesByEVMAddress(ctx, caller)
	if err != nil {
		rerr = err
		return
	}

	denom := args[0].(string)
	if denom == "" {
		rerr = errors.New("invalid denom")
		return
	}

	decryptableBalance := args[1].(string)
	if decryptableBalance == "" {
		rerr = errors.New("invalid decryptable balance")
		return
	}

	pendingBalanceCreditCounter, ok := args[2].(uint32)
	if !ok {
		rerr = errors.New("invalid pendingBalanceCreditCounter")
		return
	}

	var availableBalance cttypes.Ciphertext
	err = availableBalance.Unmarshal(args[3].([]byte))
	if err != nil {
		rerr = err
		return
	}

	msg := &cttypes.MsgApplyPendingBalance{
		Address:                        fromAddr.String(),
		Denom:                          denom,
		NewDecryptableAvailableBalance: decryptableBalance,
		CurrentPendingBalanceCounter:   pendingBalanceCreditCounter,
		CurrentAvailableBalance:        &availableBalance,
	}

	_, err = p.ctKeeper.ApplyPendingBalance(sdk.WrapSDKContext(ctx), msg)
	if err != nil {
		rerr = err
		return
	}

	ret, rerr = method.Outputs.Pack(true)
	remainingGas = pcommon.GetRemainingGas(ctx, p.evmKeeper)
	return
}

func (p PrecompileExecutor) withdraw(ctx sdk.Context, method *abi.Method, caller common.Address, args []interface{}) (ret []byte, remainingGas uint64, rerr error) {
	defer func() {
		if err := recover(); err != nil {
			ret = nil
			remainingGas = 0
			rerr = fmt.Errorf("%s", err)
			return
		}
	}()

	if err := pcommon.ValidateArgsLength(args, 5); err != nil {
		rerr = err
		return
	}

	fromAddr, _, err := p.getAssociatedAddressesByEVMAddress(ctx, caller)
	if err != nil {
		rerr = err
		return
	}

	denom := args[0].(string)
	if denom == "" {
		rerr = errors.New("invalid denom")
		return
	}

	amount, ok := args[1].(*big.Int)
	if !ok {
		rerr = errors.New("invalid amount")
		return
	}

	decryptableBalance := args[2].(string)
	if decryptableBalance == "" {
		rerr = errors.New("invalid decryptable balance")
		return
	}

	var remainingBalanceCommitment cttypes.Ciphertext
	err = remainingBalanceCommitment.Unmarshal(args[3].([]byte))
	if err != nil {
		rerr = errors.New("invalid remainingBalanceCommitment")
		return
	}

	var withdrawProofs cttypes.WithdrawMsgProofs
	err = withdrawProofs.Unmarshal(args[4].([]byte))
	if err != nil {
		rerr = err
		return
	}

	msg := &cttypes.MsgWithdraw{
		FromAddress:                fromAddr.String(),
		Denom:                      denom,
		Amount:                     amount.String(),
		DecryptableBalance:         decryptableBalance,
		RemainingBalanceCommitment: &remainingBalanceCommitment,
		Proofs:                     &withdrawProofs,
	}

	_, err = p.ctKeeper.Withdraw(sdk.WrapSDKContext(ctx), msg)
	if err != nil {
		rerr = err
		return
	}

	ret, rerr = method.Outputs.Pack(true)
	remainingGas = pcommon.GetRemainingGas(ctx, p.evmKeeper)
	return
}

func (p PrecompileExecutor) closeAccount(ctx sdk.Context, method *abi.Method, caller common.Address, args []interface{}) (ret []byte, remainingGas uint64, rerr error) {
	defer func() {
		if err := recover(); err != nil {
			ret = nil
			remainingGas = 0
			rerr = fmt.Errorf("%s", err)
			return
		}
	}()

	if err := pcommon.ValidateArgsLength(args, 2); err != nil {
		rerr = err
		return
	}

	fromAddr, _, err := p.getAssociatedAddressesByEVMAddress(ctx, caller)
	if err != nil {
		rerr = err
		return
	}

	denom := args[0].(string)
	if denom == "" {
		rerr = errors.New("invalid denom")
		return
	}

	var closeAccountProofs cttypes.CloseAccountMsgProofs
	err = closeAccountProofs.Unmarshal(args[1].([]byte))
	if err != nil {
		rerr = err
		return
	}

	msg := &cttypes.MsgCloseAccount{
		Address: fromAddr.String(),
		Denom:   denom,
		Proofs:  &closeAccountProofs,
	}

	_, err = p.ctKeeper.CloseAccount(sdk.WrapSDKContext(ctx), msg)
	if err != nil {
		rerr = err
		return
	}
	ret, rerr = method.Outputs.Pack(true)
	remainingGas = pcommon.GetRemainingGas(ctx, p.evmKeeper)
	return
}
