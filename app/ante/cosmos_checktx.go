package ante

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/sei-protocol/sei-chain/app/antedecorators"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	codectypes "github.com/sei-protocol/sei-chain/sei-cosmos/codec/types"
	kmultisig "github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keys/multisig"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keys/secp256k1"
	cryptotypes "github.com/sei-protocol/sei-chain/sei-cosmos/crypto/types"
	storetypes "github.com/sei-protocol/sei-chain/sei-cosmos/store/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	sdkerrors "github.com/sei-protocol/sei-chain/sei-cosmos/types/errors"
	"github.com/sei-protocol/sei-chain/sei-cosmos/types/tx/signing"
	authante "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/ante"
	authkeeper "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/keeper"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/legacy/legacytx"
	authsigning "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/signing"
	authtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/authz"
	bankkeeper "github.com/sei-protocol/sei-chain/sei-cosmos/x/bank/keeper"
	paramskeeper "github.com/sei-protocol/sei-chain/sei-cosmos/x/params/keeper"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/sei-protocol/sei-chain/utils/helpers"
	evmkeeper "github.com/sei-protocol/sei-chain/x/evm/keeper"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

const (
	maxNestedMsgs    = 5
	maxNestedPubKeys = 5
)

var (
	_ GasTx = (*legacytx.StdTx)(nil) // assert StdTx implements GasTx
)

// GasTx defines a Tx with a GetGas() method which is needed to use SetUpContextDecorator
type GasTx interface {
	sdk.Tx
	GetGas() uint64
}

type HasExtensionOptionsTx interface {
	GetExtensionOptions() []*codectypes.Any
	GetNonCriticalExtensionOptions() []*codectypes.Any
}

// TxWithTimeoutHeight defines the interface a tx must implement in order for
// TxHeightTimeoutDecorator to process the tx.
type TxWithTimeoutHeight interface {
	sdk.Tx

	GetTimeoutHeight() uint64
}

func CosmosCheckTxAnte(
	ctx sdk.Context,
	txConfig client.TxConfig,
	tx sdk.Tx,
	pk paramskeeper.Keeper,
	ek *evmkeeper.Keeper,
	accountKeeper authkeeper.AccountKeeper,
	bankKeeper bankkeeper.Keeper,
) (returnCtx sdk.Context, returnErr error) {
	// Auth params are needed for stateless checks before SetGasMeter installs the
	// tx meter. Read them on a throwaway meter so this early lookup does not
	// charge the incoming caller/block meter.
	authParams := accountKeeper.GetParams(ctx.WithGasMeter(storetypes.NewNoConsumptionInfiniteGasMeter()))

	if err := CosmosStatelessChecks(tx, ctx.BlockHeight(), ctx.ConsensusParams(), authParams); err != nil {
		return SetGasMeter(ctx, 0, pk), err
	}

	defer func() {
		if r := recover(); r != nil {
			returnErr = HandleOutofGas(r, tx.(GasTx).GetGas(), ctx.GasMeter().GasConsumed())
		}
	}()
	ctx = SetGasMeter(ctx, tx.(GasTx).GetGas(), pk)

	if err := CheckMemoLength(tx, authParams); err != nil {
		return ctx, err
	}

	ctx.GasMeter().ConsumeGas(authParams.TxSizeCostPerByte*sdk.Gas(len(ctx.TxBytes())), "txSize")

	signerAccounts, err := CheckPubKeys(ctx, tx, accountKeeper, authParams)
	if err != nil {
		return ctx, err
	}

	if _, err := CheckSignatures(ctx, txConfig, tx, signerAccounts, authParams); err != nil {
		return ctx, err
	}

	if _, err := UpdateSigners(ctx, tx, accountKeeper, ek); err != nil {
		return ctx, err
	}

	priority, err := CheckAndChargeFees(ctx, tx, accountKeeper, bankKeeper, pk)
	if err != nil {
		return ctx, err
	}
	ctx = DecoratePriority(ctx, priority)

	return ctx, nil
}

func HandleOutofGas(recoveredErr any, gasLimit uint64, gasConsumed uint64) error {
	switch rType := recoveredErr.(type) {
	case sdk.ErrorOutOfGas:
		log := fmt.Sprintf(
			"out of gas in location: %v; gasWanted: %d, gasUsed: %d",
			rType.Descriptor, gasLimit, gasConsumed)

		return sdkerrors.Wrap(sdkerrors.ErrOutOfGas, log)
	default:
		panic(recoveredErr)
	}
}

func CosmosStatelessChecks(tx sdk.Tx, height int64, consensusParams *tmproto.ConsensusParams, authParams authtypes.Params) error {
	gasTx, ok := tx.(GasTx)
	if !ok {
		return sdkerrors.Wrap(sdkerrors.ErrTxDecode, "Tx must be GasTx")
	}
	if cp := consensusParams; cp != nil && cp.Block != nil {
		// If there exists a maximum block gas limit, we must ensure that the tx
		// does not exceed it.
		if cp.Block.MaxGas > 0 && gasTx.GetGas() > uint64(cp.Block.MaxGas) { //nolint:gosec
			return sdkerrors.Wrapf(sdkerrors.ErrOutOfGas, "tx gas wanted %d exceeds block max gas limit %d", gasTx.GetGas(), cp.Block.MaxGas)
		}
	}
	_, ok = tx.(sdk.FeeTx)
	if !ok {
		return sdkerrors.Wrap(sdkerrors.ErrTxDecode, "Tx must be a FeeTx")
	}
	if hasExtOptsTx, ok := tx.(HasExtensionOptionsTx); ok {
		if len(hasExtOptsTx.GetExtensionOptions()) != 0 {
			return sdkerrors.ErrUnknownExtensionOptions
		}
	}
	if err := tx.ValidateBasic(); err != nil {
		return err
	}
	if len(tx.GetMsgs()) == 0 {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "must contain at least one message")
	}
	for _, msg := range tx.GetMsgs() {
		err := msg.ValidateBasic()
		if err != nil {
			return err
		}
	}
	timeoutTx, ok := tx.(TxWithTimeoutHeight)
	if !ok {
		return sdkerrors.Wrap(sdkerrors.ErrTxDecode, "expected tx to implement TxWithTimeoutHeight")
	}

	timeoutHeight := timeoutTx.GetTimeoutHeight()
	if timeoutHeight > 0 && uint64(height) > timeoutHeight { //nolint:gosec
		return sdkerrors.Wrapf(
			sdkerrors.ErrTxTimeoutHeight, "block height: %d, timeout height: %d", height, timeoutHeight,
		)
	}
	_, ok = tx.(sdk.TxWithMemo)
	if !ok {
		return sdkerrors.Wrap(sdkerrors.ErrTxDecode, "invalid transaction type")
	}
	sigTx, ok := tx.(authsigning.SigVerifiableTx)
	if !ok {
		return sdkerrors.Wrap(sdkerrors.ErrTxDecode, "invalid tx type")
	}
	pubkeys, err := sigTx.GetPubKeys()
	if err != nil {
		return err
	}
	// Validate all provided public keys before deriving addresses from them below.
	// Keep the recursive work budget local to each pubkey: this bounds a single
	// multisig tree without changing CheckPubKeys' aggregate TxSigLimit behavior
	// for pubkeys that are persisted to account state.
	for _, pk := range pubkeys {
		// PublicKey was omitted from slice since it has already been set in context
		if pk == nil {
			continue
		}
		remainingSigCount := authParams.TxSigLimit
		if err := validatePubKey(pk, &remainingSigCount, 0); err != nil {
			return err
		}
	}

	signers := sigTx.GetSigners()
	for i, pk := range pubkeys {
		// PublicKey was omitted from slice since it has already been set in context
		if pk == nil {
			continue
		}
		if !bytes.Equal(pk.Address(), signers[i]) {
			return sdkerrors.Wrapf(sdkerrors.ErrInvalidPubKey,
				"pubKey does not match signer address %s with signer index: %d", signers[i], i)
		}
	}

	for _, msg := range tx.GetMsgs() {
		switch m := msg.(type) {
		case *authz.MsgExec:
			// find nested evm messages
			containsEvm, err := CheckAuthzContainsEvm(m, 0)
			if err != nil {
				return err
			}
			if containsEvm {
				return errors.New("permission denied, authz tx contains evm message")
			}
		default:
			continue
		}
	}
	return nil
}

func validatePubKey(pubKey cryptotypes.PubKey, remainingSigCount *uint64, depth int) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = sdkerrors.Wrapf(sdkerrors.ErrInvalidPubKey, "invalid public key: %v", r)
		}
	}()

	switch pk := pubKey.(type) {
	case nil:
		return sdkerrors.Wrap(sdkerrors.ErrInvalidPubKey, "missing public key")
	case *kmultisig.LegacyAminoPubKey:
		if pk == nil {
			return sdkerrors.Wrap(sdkerrors.ErrInvalidPubKey, "missing multisig public key")
		}
		if depth >= maxNestedPubKeys {
			return sdkerrors.Wrapf(sdkerrors.ErrInvalidPubKey, "multisig public key nesting exceeds limit %d", maxNestedPubKeys)
		}

		pubKeyCount := len(pk.PubKeys)
		threshold := pk.GetThreshold()
		if threshold == 0 {
			return sdkerrors.Wrap(sdkerrors.ErrInvalidPubKey, "multisig threshold must be positive")
		}
		if threshold > uint(pubKeyCount) {
			return sdkerrors.Wrapf(sdkerrors.ErrInvalidPubKey, "multisig threshold %d exceeds public key count %d", threshold, pubKeyCount)
		}
		if remainingSigCount != nil && *remainingSigCount < uint64(pubKeyCount) { //nolint:gosec // pubKeyCount is bounded by tx size and compared to tx sig limit.
			return sdkerrors.Wrapf(sdkerrors.ErrTooManySignatures, "signatures exceed limit %d", *remainingSigCount)
		}

		for i, packedKey := range pk.PubKeys {
			if packedKey == nil {
				return sdkerrors.Wrapf(sdkerrors.ErrInvalidPubKey, "missing multisig public key at index %d", i)
			}
			child, ok := packedKey.GetCachedValue().(cryptotypes.PubKey)
			if !ok {
				return sdkerrors.Wrapf(sdkerrors.ErrInvalidPubKey, "invalid multisig public key at index %d", i)
			}
			if err := validatePubKey(child, remainingSigCount, depth+1); err != nil {
				return sdkerrors.Wrapf(err, "invalid multisig public key at index %d", i)
			}
		}

		return nil
	case *secp256k1.PubKey:
		if pk == nil {
			return sdkerrors.Wrap(sdkerrors.ErrInvalidPubKey, "missing secp256k1 public key")
		}
		if remainingSigCount != nil {
			if *remainingSigCount == 0 {
				return sdkerrors.Wrap(sdkerrors.ErrTooManySignatures, "signatures exceed limit")
			}
			*remainingSigCount--
		}
		if len(pk.Key) != secp256k1.PubKeySize {
			return sdkerrors.Wrapf(sdkerrors.ErrInvalidPubKey, "invalid secp256k1 public key size %d", len(pk.Key))
		}
		if _, err := btcec.ParsePubKey(pk.Key); err != nil {
			return sdkerrors.Wrap(sdkerrors.ErrInvalidPubKey, "invalid secp256k1 public key")
		}
		return nil
	default:
		if remainingSigCount != nil {
			if *remainingSigCount == 0 {
				return sdkerrors.Wrap(sdkerrors.ErrTooManySignatures, "signatures exceed limit")
			}
			*remainingSigCount--
		}
		if len(pubKey.Address()) == 0 {
			return sdkerrors.Wrapf(sdkerrors.ErrInvalidPubKey, "invalid public key type: %T", pubKey)
		}

		return nil
	}
}

func SetGasMeter(ctx sdk.Context, gasLimit uint64, paramsKeeper paramskeeper.Keeper) sdk.Context {
	cosmosGasParams := paramsKeeper.GetCosmosGasParams(ctx)
	return ctx.WithGasMeter(storetypes.NewMultiplierGasMeter(gasLimit, cosmosGasParams.CosmosGasMultiplierNumerator, cosmosGasParams.CosmosGasMultiplierDenominator))
}

func CheckAndChargeFees(ctx sdk.Context, tx sdk.Tx, accountKeeper authkeeper.AccountKeeper, bankKeeper bankkeeper.Keeper, paramsKeeper paramskeeper.Keeper) (priority int64, err error) {
	feeTx := tx.(sdk.FeeTx)
	feeCoins := feeTx.GetFee()
	feeParams := paramsKeeper.GetFeesParams(ctx)
	feeCoins = feeCoins.NonZeroAmountsOf(append([]string{sdk.DefaultBondDenom}, feeParams.GetAllowedFeeDenoms()...))
	gas := feeTx.GetGas()
	minGasPrices := authante.GetMinimumGasPricesWantedSorted(feeParams.GetGlobalMinimumGasPrices(), ctx.MinGasPrices())
	if !minGasPrices.IsZero() {
		requiredFees := make(sdk.Coins, len(minGasPrices))

		// Determine the required fees by multiplying each required minimum gas
		// price by the gas limit, where fee = ceil(minGasPrice * gasLimit).
		glDec := sdk.NewDec(int64(gas)) //nolint:gosec // G115: gas is bounded by block gas limit, cannot overflow int64
		for i, gp := range minGasPrices {
			fee := gp.Amount.Mul(glDec)
			requiredFees[i] = sdk.NewCoin(gp.Denom, fee.Ceil().RoundInt())
		}

		if !feeCoins.IsAnyGTE(requiredFees) {
			return priority, sdkerrors.Wrapf(sdkerrors.ErrInsufficientFee, "insufficient fees; got: %s required: %s", feeCoins, requiredFees)
		}
	}
	if gas > 0 {
		priority = authante.GetTxPriority(feeCoins, int64(gas)) //nolint:gosec
	}
	if addr := accountKeeper.GetModuleAddress(authtypes.FeeCollectorName); addr == nil {
		return priority, fmt.Errorf("fee collector module account (%s) has not been set", authtypes.FeeCollectorName)
	}

	if _, err := chargeFees(ctx, tx, feeCoins, accountKeeper, bankKeeper); err != nil {
		return priority, err
	}
	return priority, nil
}

func chargeFees(ctx sdk.Context, tx sdk.Tx, feeCoins sdk.Coins, accountKeeper authkeeper.AccountKeeper, bankKeeper bankkeeper.Keeper) (sdk.AccAddress, error) {
	if addr := accountKeeper.GetModuleAddress(authtypes.FeeCollectorName); addr == nil {
		return nil, fmt.Errorf("fee collector module account (%s) has not been set", authtypes.FeeCollectorName)
	}

	feeTx := tx.(sdk.FeeTx)
	feePayer := feeTx.FeePayer()

	deductFeesFromAcc := accountKeeper.GetAccount(ctx, feePayer)
	if deductFeesFromAcc == nil {
		return nil, sdkerrors.ErrUnknownAddress.Wrapf("fee payer address: %s does not exist", feePayer)
	}

	// deduct the fees
	if !feeCoins.IsZero() {
		if !feeCoins.IsValid() {
			return nil, sdkerrors.Wrapf(sdkerrors.ErrInsufficientFee, "invalid fee amount: %s", feeCoins)
		}

		err := bankKeeper.DeferredSendCoinsFromAccountToModule(ctx, deductFeesFromAcc.GetAddress(), authtypes.FeeCollectorName, feeCoins)
		if err != nil {
			return nil, sdkerrors.Wrapf(sdkerrors.ErrInsufficientFunds, "%s", err.Error())
		}
	}

	return feePayer, nil
}

func DecoratePriority(ctx sdk.Context, priority int64) sdk.Context {
	if priority > antedecorators.MaxPriority {
		return ctx.WithPriority(antedecorators.MaxPriority)
	}
	return ctx.WithPriority(priority)
}

func CheckMemoLength(tx sdk.Tx, authParams authtypes.Params) error {
	memoLength := len(tx.(sdk.TxWithMemo).GetMemo())
	if uint64(memoLength) > authParams.MaxMemoCharacters {
		return sdkerrors.Wrapf(sdkerrors.ErrMemoTooLarge,
			"maximum number of characters is %d but received %d characters",
			authParams.MaxMemoCharacters, memoLength,
		)
	}
	return nil
}

func CheckPubKeys(ctx sdk.Context, tx sdk.Tx, accountKeeper authkeeper.AccountKeeper, authParams authtypes.Params) ([]authtypes.AccountI, error) {
	sigCount := 0
	pubkeys, err := tx.(authsigning.SigVerifiableTx).GetPubKeys()
	if err != nil {
		return nil, err
	}
	signers := tx.(authsigning.SigVerifiableTx).GetSigners()
	signerAcounts := make([]authtypes.AccountI, len(signers))
	for i, pk := range pubkeys {
		acc, err := authante.GetSignerAcc(ctx, accountKeeper, signers[i])
		if err != nil {
			return nil, err
		}
		if pk == nil || acc.GetPubKey() != nil {
			signerAcounts[i] = acc
			continue
		}
		// Normal CheckTx/DeliverTx callers already validate provided pubkeys in
		// CosmosStatelessChecks. Revalidate here as a defensive guard because the
		// next step persists this pubkey to account state.
		if err := validatePubKey(pk, nil, 0); err != nil {
			return nil, err
		}
		err = acc.SetPubKey(pk)
		if err != nil {
			return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidPubKey, err.Error())
		}
		accountKeeper.SetAccount(ctx, acc)
		signerAcounts[i] = acc

		sigCount += authante.CountSubKeys(pk)
		if uint64(sigCount) > authParams.TxSigLimit { //nolint:gosec
			return nil, sdkerrors.Wrapf(sdkerrors.ErrTooManySignatures,
				"signatures: %d, limit: %d", sigCount, authParams.TxSigLimit)
		}
	}
	return signerAcounts, nil
}

func CheckSignatures(ctx sdk.Context, txConfig client.TxConfig, tx sdk.Tx, signerAccounts []authtypes.AccountI, authParams authtypes.Params) (sdk.Events, error) {
	sigTx := tx.(authsigning.SigVerifiableTx)
	sigs, err := sigTx.GetSignaturesV2()
	if err != nil {
		return nil, err
	}

	// stdSigs contains the sequence number, account number, and signatures.
	// When simulating, this would just be a 0-length slice.
	signerAddrs := sigTx.GetSigners()
	// check that signer length and signature length are the same
	if len(sigs) != len(signerAddrs) {
		return nil, sdkerrors.Wrapf(sdkerrors.ErrUnauthorized, "invalid number of signer;  expected: %d, got %d", len(signerAddrs), len(sigs))
	}
	var events sdk.Events
	// CheckTx and ReCheckTx discard these events (see CosmosCheckTxAnte); building them
	// still runs SignatureDataToBz + base64 per signer — measurable CPU/alloc on hot path.
	skipSigEvents := ctx.IsCheckTx() || ctx.IsReCheckTx()
	for i, sig := range sigs {
		if !skipSigEvents {
			events = append(events, sdk.NewEvent(sdk.EventTypeTx,
				sdk.NewAttribute(sdk.AttributeKeyAccountSequence, fmt.Sprintf("%s/%d", signerAddrs[i], sig.Sequence)),
			))
			if sigBzs, err := authante.SignatureDataToBz(sig.Data); err != nil {
				return nil, err
			} else {
				for _, sigBz := range sigBzs {
					events = append(events, sdk.NewEvent(sdk.EventTypeTx,
						sdk.NewAttribute(sdk.AttributeKeySignature, base64.StdEncoding.EncodeToString(sigBz)),
					))
				}
			}
		}

		signerAcc := signerAccounts[i]

		pubKey := signerAcc.GetPubKey()

		// make a SignatureV2 with PubKey filled in from above
		sig = signing.SignatureV2{
			PubKey:   pubKey,
			Data:     sig.Data,
			Sequence: sig.Sequence,
		}

		err = authante.DefaultSigVerificationGasConsumer(ctx.GasMeter(), sig, authParams)
		if err != nil {
			return nil, err
		}

		// Check account sequence number.
		if sig.Sequence != signerAcc.GetSequence() {
			if !authParams.GetDisableSeqnoCheck() {
				return nil, sdkerrors.Wrapf(
					sdkerrors.ErrWrongSequence,
					"account sequence mismatch, expected %d, got %d", signerAcc.GetSequence(), sig.Sequence,
				)
			}
		}

		if ctx.IsReCheckTx() {
			continue
		}

		// retrieve signer data
		chainID := ctx.ChainID()
		signerData := authsigning.SignerData{
			ChainID:       chainID,
			AccountNumber: signerAcc.GetAccountNumber(),
			Sequence:      signerAcc.GetSequence(),
		}

		err = authsigning.VerifySignature(pubKey, signerData, sig.Data, txConfig.SignModeHandler(), tx)
		if err != nil {
			var errMsg string
			if authante.OnlyLegacyAminoSigners(sig.Data) {
				// If all signers are using SIGN_MODE_LEGACY_AMINO, we rely on VerifySignature to check account sequence number,
				// and therefore communicate sequence number as a potential cause of error.
				errMsg = fmt.Sprintf("signature verification failed; please verify account number (%d), sequence (%d) and chain-id (%s)", signerAcc.GetAccountNumber(), signerAcc.GetSequence(), chainID)
			} else {
				errMsg = fmt.Sprintf("signature verification failed; please verify account number (%d) and chain-id (%s)", signerAcc.GetAccountNumber(), chainID)
			}
			return nil, sdkerrors.Wrap(sdkerrors.ErrUnauthorized, errMsg)

		}
	}
	return events, nil
}

func UpdateSigners(ctx sdk.Context, tx sdk.Tx, accountKeeper authkeeper.AccountKeeper, evmKeeper *evmkeeper.Keeper) (sdk.Events, error) {
	signers := tx.(authsigning.SigVerifiableTx).GetSigners()
	var events sdk.Events
	for _, signer := range signers {
		acc := accountKeeper.GetAccount(ctx, signer)
		if err := acc.SetSequence(acc.GetSequence() + 1); err != nil {
			panic(err)
		}

		accountKeeper.SetAccount(ctx, acc)
		if evmAddr, associated := evmKeeper.GetEVMAddress(ctx, signer); associated {
			events = append(events, sdk.NewEvent(evmtypes.EventTypeSigner,
				sdk.NewAttribute(evmtypes.AttributeKeyEvmAddress, evmAddr.Hex()),
				sdk.NewAttribute(evmtypes.AttributeKeySeiAddress, signer.String())))
			continue
		}
		if acc.GetPubKey() == nil {
			logger.Error("missing pubkey for signer", "signer", signer)
			events = append(events, sdk.NewEvent(evmtypes.EventTypeSigner,
				sdk.NewAttribute(evmtypes.AttributeKeySeiAddress, signer.String())))
			continue
		}
		pk, err := btcec.ParsePubKey(acc.GetPubKey().Bytes())
		if err != nil {
			logger.Debug("failed to parse pubkey, likely due to the fact that it isn't on secp256k1 curve", "pub-key", acc.GetPubKey(), "err", err)
			events = append(events, sdk.NewEvent(evmtypes.EventTypeSigner,
				sdk.NewAttribute(evmtypes.AttributeKeySeiAddress, signer.String())))
			continue
		}
		evmAddr, err := helpers.PubkeyToEVMAddress(pk.SerializeUncompressed())
		if err != nil {
			logger.Error("failed to get EVM address from pubkey", "err", err)
			events = append(events, sdk.NewEvent(evmtypes.EventTypeSigner,
				sdk.NewAttribute(evmtypes.AttributeKeySeiAddress, signer.String())))
			continue
		}
		events = append(events, sdk.NewEvent(evmtypes.EventTypeSigner,
			sdk.NewAttribute(evmtypes.AttributeKeyEvmAddress, evmAddr.Hex()),
			sdk.NewAttribute(evmtypes.AttributeKeySeiAddress, signer.String())))
		evmKeeper.SetAddressMapping(ctx, signer, evmAddr)
		associationHelper := helpers.NewAssociationHelper(evmKeeper, evmKeeper.BankKeeper(), accountKeeper)
		if err := associationHelper.MigrateBalance(ctx, evmAddr, signer, false); err != nil {
			logger.Error("failed to migrate EVM address balance", "address", evmAddr, "err", err)
			return nil, err
		}
	}
	return events, nil
}

func CheckAuthzContainsEvm(authzMsg *authz.MsgExec, nestedLvl int) (bool, error) {
	if nestedLvl >= maxNestedMsgs {
		return false, errors.New("permission denied, more nested msgs than permitted")
	}
	msgs, err := authzMsg.GetMessages()
	if err != nil {
		return false, err
	}
	for _, msg := range msgs {
		// check if message type is authz exec or evm
		switch m := msg.(type) {
		case *evmtypes.MsgEVMTransaction:
			return true, nil
		case *authz.MsgExec:
			// find nested to check for evm
			valid, err := CheckAuthzContainsEvm(m, nestedLvl+1)
			if err != nil {
				return false, err
			}
			if valid {
				return true, nil
			}
		default:
			continue
		}
	}
	return false, nil
}
