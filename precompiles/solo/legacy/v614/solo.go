package v614

import (
	"bytes"
	"embed"
	"encoding/json"
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
	pcommon "github.com/sei-protocol/sei-chain/precompiles/common/legacy/v614"
	putils "github.com/sei-protocol/sei-chain/precompiles/utils"
	"github.com/sei-protocol/sei-chain/utils"
)

const (
	ClaimMethod         = "claim"
	ClaimSpecificMethod = "claimSpecific"
)

const SoloAddress = "0x000000000000000000000000000000000000100C"

// Embed abi json file to the executable binary. Needed when importing as dependency.
//
//go:embed abi.json
var F embed.FS

type PrecompileExecutor struct {
	evmKeeper      putils.EVMKeeper
	bankKeeper     putils.BankKeeper
	accountKeeper  putils.AccountKeeper
	wasmKeeper     putils.WasmdKeeper
	wasmViewKeeper putils.WasmdViewKeeper

	txConfig client.TxConfig

	ClaimMethodID         []byte
	ClaimSpecificMethodID []byte
}

func NewPrecompile(
	keepers putils.Keepers,
) (*pcommon.DynamicGasPrecompile, error) {
	newAbi := pcommon.MustGetABI(F, "abi.json")

	return pcommon.NewDynamicGasPrecompile(
		newAbi, NewExecutor(newAbi, keepers.EVMK(), keepers.BankK(), keepers.AccountK(), keepers.WasmdK(), keepers.WasmdVK(), keepers.TxConfig()),
		common.HexToAddress(SoloAddress), "solo"), nil
}

func NewExecutor(
	a abi.ABI,
	evmKeeper putils.EVMKeeper,
	bankKeeper putils.BankKeeper,
	accountKeeper putils.AccountKeeper,
	wasmKeeper putils.WasmdKeeper,
	wasmViewKeeper putils.WasmdViewKeeper,
	txConfig client.TxConfig,
) *PrecompileExecutor {
	p := &PrecompileExecutor{
		evmKeeper:      evmKeeper,
		bankKeeper:     bankKeeper,
		accountKeeper:  accountKeeper,
		wasmKeeper:     wasmKeeper,
		wasmViewKeeper: wasmViewKeeper,
		txConfig:       txConfig,
	}

	for name, m := range a.Methods {
		switch name {
		case ClaimMethod:
			p.ClaimMethodID = m.ID
		case ClaimSpecificMethod:
			p.ClaimSpecificMethodID = m.ID
		}
	}
	return p
}

func (p PrecompileExecutor) Execute(ctx sdk.Context, method *abi.Method, caller common.Address, callingContract common.Address, args []interface{}, value *big.Int, readOnly bool, evm *vm.EVM, suppliedGas uint64, _ *tracing.Hooks) (ret []byte, remainingGas uint64, err error) {
	// Needed to catch gas meter panics
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("execution reverted: %v", r)
			ret = nil
			remainingGas = 0
			return
		}
	}()
	if !ctx.IsEVM() {
		return nil, 0, errors.New("cannot claim from a CW call")
	}
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, 0, err
	}
	if !ctx.IsEVM() || ctx.EVMEntryViaWasmdPrecompile() {
		return nil, 0, errors.New("cannot claim from cosmos entry")
	}
	if ctx.EVMPrecompileCalledFromDelegateCall() {
		return nil, 0, errors.New("cannot delegatecall claim")
	}
	// depth is incremented upon entering the precompile call so it's
	// expected to be 1.
	if evm.GetDepth() > 1 {
		return nil, 0, errors.New("claim must be called by an EOA directly")
	}
	switch method.Name {
	case ClaimMethod:
		return p.Claim(ctx, caller, method, args, readOnly)
	case ClaimSpecificMethod:
		return p.ClaimSpecific(ctx, caller, method, args, readOnly)
	}
	return
}

func (p PrecompileExecutor) EVMKeeper() putils.EVMKeeper {
	return p.evmKeeper
}

type claimMsg interface {
	GetClaimer() string
	GetSender() string
}

type claimSpecificMsg interface {
	claimMsg
	GetIAssets() []utils.IAsset
}

func (p PrecompileExecutor) Claim(ctx sdk.Context, caller common.Address, method *abi.Method, args []interface{}, readOnly bool) (ret []byte, remainingGas uint64, err error) {
	claimMsg, sender, err := p.validate(ctx, caller, args, readOnly)
	if err != nil {
		return nil, 0, err
	}
	_, ok := claimMsg.(claimSpecificMsg)
	if ok {
		return nil, 0, errors.New("message for Claim must not be MsgClaimSpecific type")
	}
	if err := p.bankKeeper.SendCoins(ctx, sender,
		p.evmKeeper.GetSeiAddressOrDefault(ctx, caller), p.bankKeeper.GetAllBalances(ctx, sender)); err != nil {
		return nil, 0, fmt.Errorf("failed to transfer coins: %w", err)
	}
	bz, err := method.Outputs.Pack(true)
	return bz, pcommon.GetRemainingGas(ctx, p.evmKeeper), err
}

func (p PrecompileExecutor) ClaimSpecific(ctx sdk.Context, caller common.Address, method *abi.Method, args []interface{}, readOnly bool) (ret []byte, remainingGas uint64, err error) {
	claimMsg, sender, err := p.validate(ctx, caller, args, readOnly)
	if err != nil {
		return nil, 0, err
	}
	claimSpecificMsg, ok := claimMsg.(claimSpecificMsg)
	if !ok {
		return nil, 0, errors.New("message is not MsgClaimSpecific type")
	}
	callerSeiAddr := p.evmKeeper.GetSeiAddressOrDefault(ctx, caller)
	for _, asset := range claimSpecificMsg.GetIAssets() {
		if asset.IsNative() {
			denom := asset.GetDenom()
			balance := p.bankKeeper.GetBalance(ctx, sender, denom)
			if !balance.IsZero() {
				if err := p.bankKeeper.SendCoins(ctx, sender, callerSeiAddr, sdk.NewCoins(balance)); err != nil {
					return nil, 0, err
				}
			}
			continue
		}
		contractAddr, err := sdk.AccAddressFromBech32(asset.GetContractAddress())
		if err != nil {
			return nil, 0, fmt.Errorf("failed to parse contract address %s: %w", asset.GetContractAddress(), err)
		}
		switch {
		case asset.IsCW20():
			res, err := p.wasmViewKeeper.QuerySmartSafe(ctx, contractAddr, CW20BalanceQueryPayload(sender))
			if err != nil {
				return nil, 0, fmt.Errorf("failed to query CW20 contract %s for balance: %w", contractAddr.String(), err)
			}
			balance, err := ParseCW20BalanceQueryResponse(res)
			if err != nil {
				return nil, 0, fmt.Errorf("failed to parse CW20 contract %s balance response: %w", contractAddr.String(), err)
			}
			_, err = p.wasmKeeper.Execute(ctx, contractAddr, sender, CW20TransferPayload(callerSeiAddr, balance), sdk.NewCoins())
			if err != nil {
				return nil, 0, fmt.Errorf("failed to transfer on CW20 contract %s: %w", contractAddr.String(), err)
			}
		case asset.IsCW721():
			allTokens := []string{}
			startAfter := ""
			for {
				res, err := p.wasmViewKeeper.QuerySmartSafe(ctx, contractAddr, CW721TokensQueryPayload(sender, startAfter))
				if err != nil {
					return nil, 0, fmt.Errorf("failed to query CW721 contract %s for all tokens: %w", contractAddr.String(), err)
				}
				tokens, err := ParseCW721TokensQueryResponse(res)
				if err != nil {
					return nil, 0, fmt.Errorf("failed to parse CW20 contract %s balance response: %w", contractAddr.String(), err)
				}
				if len(tokens) == 0 {
					break
				}
				allTokens = append(allTokens, tokens...)
				startAfter = tokens[len(tokens)-1]
			}
			for _, token := range allTokens {
				_, err := p.wasmKeeper.Execute(ctx, contractAddr, sender, CW721TransferPayload(callerSeiAddr, token), sdk.NewCoins())
				if err != nil {
					return nil, 0, fmt.Errorf("failed to transfer token %s on CW721 contract %s: %w", token, contractAddr.String(), err)
				}
			}
		}
	}
	bz, err := method.Outputs.Pack(true)
	return bz, pcommon.GetRemainingGas(ctx, p.evmKeeper), err
}

func (p PrecompileExecutor) validate(ctx sdk.Context, caller common.Address, args []interface{}, readOnly bool) (claimMsg, sdk.AccAddress, error) {
	if readOnly {
		return nil, nil, errors.New("cannot call send from staticcall")
	}
	if err := pcommon.ValidateArgsLength(args, 1); err != nil {
		return nil, nil, err
	}
	tx, err := p.txConfig.TxDecoder()(args[0].([]byte))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to decode claim tx due to %w", err)
	}
	if len(tx.GetMsgs()) != 1 {
		return nil, nil, fmt.Errorf("claim tx must contain exactly 1 message but %d were found", len(tx.GetMsgs()))
	}
	claimMsg, ok := tx.GetMsgs()[0].(claimMsg)
	if !ok {
		return nil, nil, errors.New("claim tx can only contain MsgClaim or MsgClaimSpecific type")
	}
	if common.HexToAddress(claimMsg.GetClaimer()).Cmp(caller) != 0 {
		return nil, nil, fmt.Errorf("claim tx is meant for %s but was sent by %s", claimMsg.GetClaimer(), caller.Hex())
	}
	sender, err := sdk.AccAddressFromBech32(claimMsg.GetSender())
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse claim tx sender due to %s", err)
	}
	if err := p.sigverify(ctx, tx, claimMsg, sender); err != nil {
		return nil, nil, err
	}
	return claimMsg, sender, nil
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
	pubkeyAddr := sdk.AccAddress(pubkey.Address())
	if !bytes.Equal(pubkeyAddr, sender) {
		return fmt.Errorf("claim message is for %s but was signed by %s", sender.String(), pubkeyAddr.String())
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
	// increment sequence
	_ = acct.SetSequence(acct.GetSequence() + 1)
	p.accountKeeper.SetAccount(ctx, acct)
	return nil
}

func CW20BalanceQueryPayload(addr sdk.AccAddress) []byte {
	raw := map[string]interface{}{"address": addr.String()}
	bz, err := json.Marshal(map[string]interface{}{"balance": raw})
	if err != nil {
		// should be impossible
		panic(err)
	}
	return bz
}

func ParseCW20BalanceQueryResponse(res []byte) (sdk.Int, error) {
	type response struct {
		Balance sdk.Int `json:"balance"`
	}
	typed := response{}
	if err := json.Unmarshal(res, &typed); err != nil {
		return sdk.Int{}, err
	}
	return typed.Balance, nil
}

func CW20TransferPayload(recipient sdk.AccAddress, amount sdk.Int) []byte {
	type request struct {
		Recipient string  `json:"recipient"`
		Amount    sdk.Int `json:"amount"`
	}
	raw := request{Recipient: recipient.String(), Amount: amount}
	bz, err := json.Marshal(map[string]interface{}{"transfer": raw})
	if err != nil {
		// should be impossible
		panic(err)
	}
	return bz
}

func CW721TokensQueryPayload(addr sdk.AccAddress, startAfter string) []byte {
	raw := map[string]interface{}{"owner": addr.String()}
	if startAfter != "" {
		raw["start_after"] = startAfter
	}
	bz, err := json.Marshal(map[string]interface{}{"tokens": raw})
	if err != nil {
		// should be impossible
		panic(err)
	}
	return bz
}

func ParseCW721TokensQueryResponse(res []byte) ([]string, error) {
	type response struct {
		Tokens []string `json:"tokens"`
	}
	typed := response{}
	if err := json.Unmarshal(res, &typed); err != nil {
		return []string{}, err
	}
	return typed.Tokens, nil
}

func CW721TransferPayload(recipient sdk.AccAddress, token string) []byte {
	type request struct {
		Recipient string `json:"recipient"`
		Token     string `json:"token_id"`
	}
	raw := request{Recipient: recipient.String(), Token: token}
	bz, err := json.Marshal(map[string]interface{}{"transfer_nft": raw})
	if err != nil {
		// should be impossible
		panic(err)
	}
	return bz
}
