package solo

import (
	"embed"
	"errors"
	"fmt"
	"math/big"

	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"

	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	authante "github.com/cosmos/cosmos-sdk/x/auth/ante"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/vm"
	pcommon "github.com/sei-protocol/sei-chain/precompiles/common"
)

const (
	ClaimMethod = "claim"
)

const SoloAddress = "0x000000000000000000000000000000000000100C"

// Embed abi json file to the executable binary. Needed when importing as dependency.
//
//go:embed abi.json
var F embed.FS

type PrecompileExecutor struct {
	evmKeeper     pcommon.EVMKeeper
	bankKeeper    pcommon.BankKeeper
	accountKeeper pcommon.AccountKeeper

	txConfig client.TxConfig

	ClaimMethodID []byte
}

func NewPrecompile(
	evmKeeper pcommon.EVMKeeper,
	bankKeeper pcommon.BankKeeper,
	accountKeeper pcommon.AccountKeeper,
	txConfig client.TxConfig,
) (*pcommon.DynamicGasPrecompile, error) {
	newAbi := pcommon.MustGetABI(F, "abi.json")

	return pcommon.NewDynamicGasPrecompile(
		newAbi, NewExecutor(newAbi, evmKeeper, bankKeeper, accountKeeper, txConfig),
		common.HexToAddress(SoloAddress), "solo"), nil
}

func NewExecutor(
	a abi.ABI,
	evmKeeper pcommon.EVMKeeper,
	bankKeeper pcommon.BankKeeper,
	accountKeeper pcommon.AccountKeeper,
	txConfig client.TxConfig,
) *PrecompileExecutor {
	p := &PrecompileExecutor{
		evmKeeper:     evmKeeper,
		bankKeeper:    bankKeeper,
		accountKeeper: accountKeeper,
		txConfig:      txConfig,
	}

	for name, m := range a.Methods {
		switch name {
		case ClaimMethod:
			p.ClaimMethodID = m.ID
		}
	}
	return p
}

func (p PrecompileExecutor) Execute(ctx sdk.Context, method *abi.Method, caller common.Address, callingContract common.Address, args []interface{}, value *big.Int, readOnly bool, evm *vm.EVM, suppliedGas uint64, _ *tracing.Hooks) (ret []byte, remainingGas uint64, err error) {
	// Needed to catch gas meter panics
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("execution reverted: %v", r)
		}
	}()
	switch method.Name {
	case ClaimMethod:
		return p.Claim(ctx, caller, method, args, readOnly)
	}
	return
}

func (p PrecompileExecutor) EVMKeeper() pcommon.EVMKeeper {
	return p.evmKeeper
}

type claimMsg interface {
	GetClaimer() string
	GetSender() string
}

func (p PrecompileExecutor) Claim(ctx sdk.Context, caller common.Address, method *abi.Method, args []interface{}, readOnly bool) (ret []byte, remainingGas uint64, err error) {
	if readOnly {
		return nil, 0, errors.New("cannot call send from staticcall")
	}
	if err := pcommon.ValidateArgsLength(args, 1); err != nil {
		return nil, 0, err
	}
	tx, err := p.txConfig.TxDecoder()(args[0].([]byte))
	if err != nil {
		return nil, 0, fmt.Errorf("failed to decode claim tx due to %w", err)
	}
	if len(tx.GetMsgs()) != 1 {
		return nil, 0, fmt.Errorf("claim tx must contain exactly 1 message but %d were found", len(tx.GetMsgs()))
	}
	claimMsg, ok := tx.GetMsgs()[0].(claimMsg)
	if !ok {
		return nil, 0, errors.New("claim tx can only contain MsgClaim type")
	}
	if common.HexToAddress(claimMsg.GetClaimer()).Cmp(caller) != 0 {
		return nil, 0, fmt.Errorf("claim tx is meant for %s but was sent by %s", claimMsg.GetClaimer(), caller.Hex())
	}
	sender, err := sdk.AccAddressFromBech32(claimMsg.GetSender())
	if err != nil {
		return nil, 0, fmt.Errorf("failed to parse claim tx sender due to %s", err)
	}
	if err := p.sigverify(ctx, tx, claimMsg, sender); err != nil {
		return nil, 0, err
	}
	if err := p.bankKeeper.SendCoins(ctx, sender,
		p.evmKeeper.GetSeiAddressOrDefault(ctx, caller), p.bankKeeper.GetAllBalances(ctx, sender)); err != nil {
		return nil, 0, fmt.Errorf("failed to transfer coins: %w", err)
	}
	bz, err := method.Outputs.Pack(true)
	return bz, pcommon.GetRemainingGas(ctx, p.evmKeeper), err
}

func (p PrecompileExecutor) sigverify(ctx sdk.Context, tx sdk.Tx, claimMsg claimMsg, sender sdk.AccAddress) error {
	sigTx, ok := tx.(authsigning.SigVerifiableTx)
	if !ok {
		return errors.New("claim tx must be a SigVerifiableTx")
	}
	var pubkey cryptotypes.PubKey
	pubkeys, err := sigTx.GetPubKeys()
	acct := p.accountKeeper.GetAccount(ctx, sender)
	if acct == nil {
		return fmt.Errorf("account %s does not exist", claimMsg.GetSender())
	}
	if err == nil {
		if len(pubkeys) != 1 {
			return fmt.Errorf("claim tx can be signed by exactly 1 key but %d signed", len(pubkeys))
		}
		pubkey = pubkeys[0]
	} else {
		// try find pubkey from storage
		pubkey = acct.GetPubKey()
		if pubkey == nil {
			return errors.New("must provide pubkey from accounts that have never sent transactions")
		}
	}
	sigs, err := sigTx.GetSignaturesV2()
	if err != nil {
		return fmt.Errorf("failed to get signatures due to %w", err)
	}
	if len(sigs) != 1 {
		return fmt.Errorf("claim tx should have exactly 1 signature but got %d", len(sigs))
	}
	sig := sigs[0]
	if sig.Sequence != acct.GetSequence() {
		return fmt.Errorf("account sequence mismatch for claim tx (%d vs. %d)", sig.Sequence, acct.GetSequence())
	}
	if err := authante.DefaultSigVerificationGasConsumer(ctx.GasMeter(), sig, p.accountKeeper.GetParams(ctx)); err != nil {
		return fmt.Errorf("insufficient gas for sig verification: %w", err)
	}
	signerData := authsigning.SignerData{
		ChainID:       ctx.ChainID(),
		AccountNumber: acct.GetAccountNumber(),
		Sequence:      acct.GetSequence(),
	}
	if err := authsigning.VerifySignature(pubkey, signerData, sig.Data, p.txConfig.SignModeHandler(), tx); err != nil {
		return fmt.Errorf("failed to verify signature for claim tx: %w", err)
	}
	return nil
}
