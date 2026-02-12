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
	feegrantkeeper "github.com/sei-protocol/sei-chain/sei-cosmos/x/feegrant/keeper"
	paramskeeper "github.com/sei-protocol/sei-chain/sei-cosmos/x/params/keeper"
	clienttypes "github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/02-client/types"
	channeltypes "github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/04-channel/types"
	ibckeeper "github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/keeper"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/sei-protocol/sei-chain/utils/helpers"
	evmkeeper "github.com/sei-protocol/sei-chain/x/evm/keeper"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	oraclekeeper "github.com/sei-protocol/sei-chain/x/oracle/keeper"
	oracletypes "github.com/sei-protocol/sei-chain/x/oracle/types"
)

const maxNestedMsgs = 5

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
	oraclek oraclekeeper.Keeper,
	ek *evmkeeper.Keeper,
	accountKeeper authkeeper.AccountKeeper,
	bankKeeper bankkeeper.Keeper,
	feegrantKeeper *feegrantkeeper.Keeper,
	ibcKeeper *ibckeeper.Keeper,
) (returnCtx sdk.Context, returnErr error) {
	oracleVote, err := CosmosStatelessChecks(tx, ctx.BlockHeight(), ctx.ConsensusParams())
	if err != nil {
		return SetGasMeter(ctx, 0, pk), err
	}

	defer func() {
		if r := recover(); r != nil {
			returnErr = HandleOutofGas(r, tx.(GasTx).GetGas(), ctx.GasMeter().GasConsumed())
		}
	}()
	ctx = ctx.WithGasMeter(storetypes.NewNoConsumptionInfiniteGasMeter())
	isGasless, err := antedecorators.IsTxGasless(tx, ctx, oraclek, ek)
	if err != nil {
		return ctx, err
	}
	if !isGasless {
		ctx = SetGasMeter(ctx, tx.(GasTx).GetGas(), pk)
	}

	authParams := accountKeeper.GetParams(ctx)

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

	priority, err := CheckAndChargeFees(ctx, tx, accountKeeper, bankKeeper, feegrantKeeper, pk, isGasless)
	if err != nil {
		return ctx, err
	}
	ctx = DecoratePriority(ctx, priority, oracleVote)

	return ctx, CheckMessage(ctx, tx, ibcKeeper, oraclek)
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

func CosmosStatelessChecks(tx sdk.Tx, height int64, consensusParams *tmproto.ConsensusParams) (
	isOracleVote bool, err error,
) {
	gasTx, ok := tx.(GasTx)
	if !ok {
		return false, sdkerrors.Wrap(sdkerrors.ErrTxDecode, "Tx must be GasTx")
	}
	if cp := consensusParams; cp != nil && cp.Block != nil {
		// If there exists a maximum block gas limit, we must ensure that the tx
		// does not exceed it.
		if cp.Block.MaxGas > 0 && gasTx.GetGas() > uint64(cp.Block.MaxGas) { //nolint:gosec
			return false, sdkerrors.Wrapf(sdkerrors.ErrOutOfGas, "tx gas wanted %d exceeds block max gas limit %d", gasTx.GetGas(), cp.Block.MaxGas)
		}
	}
	_, ok = tx.(sdk.FeeTx)
	if !ok {
		return false, sdkerrors.Wrap(sdkerrors.ErrTxDecode, "Tx must be a FeeTx")
	}
	if hasExtOptsTx, ok := tx.(HasExtensionOptionsTx); ok {
		if len(hasExtOptsTx.GetExtensionOptions()) != 0 {
			return false, sdkerrors.ErrUnknownExtensionOptions
		}
	}
	oracleVote := false
	otherMsg := false
	for _, msg := range tx.GetMsgs() {
		switch msg.(type) {
		case *oracletypes.MsgAggregateExchangeRateVote:
			oracleVote = true
		case *oracletypes.MsgDelegateFeedConsent:
			oracleVote = true

		default:
			otherMsg = true
		}
	}

	if oracleVote && otherMsg {
		return oracleVote, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "oracle votes cannot be in the same tx as other messages")
	}
	if err := tx.ValidateBasic(); err != nil {
		return oracleVote, err
	}
	if len(tx.GetMsgs()) == 0 {
		return oracleVote, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "must contain at least one message")
	}
	for _, msg := range tx.GetMsgs() {
		err := msg.ValidateBasic()
		if err != nil {
			return oracleVote, err
		}
	}
	timeoutTx, ok := tx.(TxWithTimeoutHeight)
	if !ok {
		return oracleVote, sdkerrors.Wrap(sdkerrors.ErrTxDecode, "expected tx to implement TxWithTimeoutHeight")
	}

	timeoutHeight := timeoutTx.GetTimeoutHeight()
	if timeoutHeight > 0 && uint64(height) > timeoutHeight { //nolint:gosec
		return oracleVote, sdkerrors.Wrapf(
			sdkerrors.ErrTxTimeoutHeight, "block height: %d, timeout height: %d", height, timeoutHeight,
		)
	}
	_, ok = tx.(sdk.TxWithMemo)
	if !ok {
		return oracleVote, sdkerrors.Wrap(sdkerrors.ErrTxDecode, "invalid transaction type")
	}
	sigTx, ok := tx.(authsigning.SigVerifiableTx)
	if !ok {
		return oracleVote, sdkerrors.Wrap(sdkerrors.ErrTxDecode, "invalid tx type")
	}
	pubkeys, err := sigTx.GetPubKeys()
	if err != nil {
		return oracleVote, err
	}
	signers := sigTx.GetSigners()

	for i, pk := range pubkeys {
		// PublicKey was omitted from slice since it has already been set in context
		if pk == nil {
			continue
		}
		if !bytes.Equal(pk.Address(), signers[i]) {
			return oracleVote, sdkerrors.Wrapf(sdkerrors.ErrInvalidPubKey,
				"pubKey does not match signer address %s with signer index: %d", signers[i], i)
		}
	}

	for _, msg := range tx.GetMsgs() {
		switch m := msg.(type) {
		case *authz.MsgExec:
			// find nested evm messages
			containsEvm, err := CheckAuthzContainsEvm(m, 0)
			if err != nil {
				return oracleVote, err
			}
			if containsEvm {
				return oracleVote, errors.New("permission denied, authz tx contains evm message")
			}
		default:
			continue
		}
	}
	return oracleVote, nil
}

func SetGasMeter(ctx sdk.Context, gasLimit uint64, paramsKeeper paramskeeper.Keeper) sdk.Context {
	cosmosGasParams := paramsKeeper.GetCosmosGasParams(ctx)

	if ctx.BlockHeight() == 0 {
		return ctx.WithGasMeter(storetypes.NewInfiniteMultiplierGasMeter(cosmosGasParams.CosmosGasMultiplierNumerator, cosmosGasParams.CosmosGasMultiplierDenominator))
	}

	return ctx.WithGasMeter(storetypes.NewMultiplierGasMeter(gasLimit, cosmosGasParams.CosmosGasMultiplierNumerator, cosmosGasParams.CosmosGasMultiplierDenominator))
}

func CheckAndChargeFees(ctx sdk.Context, tx sdk.Tx, accountKeeper authkeeper.AccountKeeper, bankKeeper bankkeeper.Keeper, feegrantKeeper *feegrantkeeper.Keeper, paramsKeeper paramskeeper.Keeper, isGasless bool) (priority int64, err error) {
	if isGasless {
		return 0, nil
	}
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
		glDec := sdk.NewDec(int64(gas))
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

	if _, err := chargeFees(ctx, tx, feeCoins, accountKeeper, bankKeeper, feegrantKeeper); err != nil {
		return priority, err
	}
	return priority, nil
}

func chargeFees(ctx sdk.Context, tx sdk.Tx, feeCoins sdk.Coins, accountKeeper authkeeper.AccountKeeper, bankKeeper bankkeeper.Keeper, feegrantKeeper *feegrantkeeper.Keeper) (sdk.AccAddress, error) {
	if addr := accountKeeper.GetModuleAddress(authtypes.FeeCollectorName); addr == nil {
		return nil, fmt.Errorf("fee collector module account (%s) has not been set", authtypes.FeeCollectorName)
	}

	feeTx := tx.(sdk.FeeTx)
	feePayer := feeTx.FeePayer()
	feeGranter := feeTx.FeeGranter()
	deductFeesFrom := feePayer

	// if feegranter set deduct fee from feegranter account.
	// this works with only when feegrant enabled.
	if feeGranter != nil {
		if feegrantKeeper == nil {
			return nil, sdkerrors.ErrInvalidRequest.Wrap("fee grants are not enabled")
		} else if !feeGranter.Equals(feePayer) {
			err := feegrantKeeper.UseGrantedFees(ctx, feeGranter, feePayer, feeCoins, tx.GetMsgs())
			if err != nil {
				return nil, sdkerrors.Wrapf(err, "%s does not not allow to pay fees for %s", feeGranter, feePayer)
			}
		}

		deductFeesFrom = feeGranter
	}

	deductFeesFromAcc := accountKeeper.GetAccount(ctx, deductFeesFrom)
	if deductFeesFromAcc == nil {
		return nil, sdkerrors.ErrUnknownAddress.Wrapf("fee payer address: %s does not exist", deductFeesFrom)
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

	return deductFeesFrom, nil
}

func DecoratePriority(ctx sdk.Context, priority int64, oracleVote bool) sdk.Context {
	if oracleVote {
		return ctx.WithPriority(antedecorators.OraclePriority)
	} else if priority > antedecorators.MaxPriority {
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
	for i, sig := range sigs {
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
		genesis := ctx.BlockHeight() == 0
		chainID := ctx.ChainID()
		var accNum uint64
		if !genesis {
			accNum = signerAcc.GetAccountNumber()
		}
		signerData := authsigning.SignerData{
			ChainID:       chainID,
			AccountNumber: accNum,
			Sequence:      signerAcc.GetSequence(),
		}

		err = authsigning.VerifySignature(pubKey, signerData, sig.Data, txConfig.SignModeHandler(), tx)
		if err != nil {
			var errMsg string
			if authante.OnlyLegacyAminoSigners(sig.Data) {
				// If all signers are using SIGN_MODE_LEGACY_AMINO, we rely on VerifySignature to check account sequence number,
				// and therefore communicate sequence number as a potential cause of error.
				errMsg = fmt.Sprintf("signature verification failed; please verify account number (%d), sequence (%d) and chain-id (%s)", accNum, signerAcc.GetSequence(), chainID)
			} else {
				errMsg = fmt.Sprintf("signature verification failed; please verify account number (%d) and chain-id (%s)", accNum, chainID)
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
			ctx.Logger().Error(fmt.Sprintf("missing pubkey for %s", signer.String()))
			events = append(events, sdk.NewEvent(evmtypes.EventTypeSigner,
				sdk.NewAttribute(evmtypes.AttributeKeySeiAddress, signer.String())))
			continue
		}
		pk, err := btcec.ParsePubKey(acc.GetPubKey().Bytes())
		if err != nil {
			ctx.Logger().Debug(fmt.Sprintf("failed to parse pubkey for %s, likely due to the fact that it isn't on secp256k1 curve", acc.GetPubKey()), "err", err)
			events = append(events, sdk.NewEvent(evmtypes.EventTypeSigner,
				sdk.NewAttribute(evmtypes.AttributeKeySeiAddress, signer.String())))
			continue
		}
		evmAddr, err := helpers.PubkeyToEVMAddress(pk.SerializeUncompressed())
		if err != nil {
			ctx.Logger().Error(fmt.Sprintf("failed to get EVM address from pubkey due to %s", err))
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
			ctx.Logger().Error(fmt.Sprintf("failed to migrate EVM address balance (%s) %s", evmAddr.Hex(), err))
			return nil, err
		}
		if evmtypes.IsTxMsgAssociate(tx) {
			// check if there is non-zero balance
			if !evmKeeper.BankKeeper().GetBalance(ctx, signer, sdk.MustGetBaseDenom()).IsPositive() && !evmKeeper.BankKeeper().GetWeiBalance(ctx, signer).IsPositive() {
				return nil, sdkerrors.Wrap(sdkerrors.ErrInsufficientFunds, "account needs to have at least 1 wei to force association")
			}
		}
	}
	return events, nil
}

func CheckMessage(ctx sdk.Context, tx sdk.Tx, ibcKeeper *ibckeeper.Keeper, oracleKeeper oraclekeeper.Keeper) error {
	// keep track of total packet messages and number of redundancies across `RecvPacket`, `AcknowledgePacket`, and `TimeoutPacket/OnClose`
	redundancies := 0
	packetMsgs := 0
	for _, m := range tx.GetMsgs() {
		switch msg := m.(type) {
		case *channeltypes.MsgRecvPacket:
			response, err := ibcKeeper.RecvPacket(sdk.WrapSDKContext(ctx), msg)
			if err != nil {
				return err
			}
			if response.Result == channeltypes.NOOP {
				redundancies += 1
			}
			packetMsgs += 1

		case *channeltypes.MsgAcknowledgement:
			response, err := ibcKeeper.Acknowledgement(sdk.WrapSDKContext(ctx), msg)
			if err != nil {
				return err
			}
			if response.Result == channeltypes.NOOP {
				redundancies += 1
			}
			packetMsgs += 1

		case *channeltypes.MsgTimeout:
			response, err := ibcKeeper.Timeout(sdk.WrapSDKContext(ctx), msg)
			if err != nil {
				return err
			}
			if response.Result == channeltypes.NOOP {
				redundancies += 1
			}
			packetMsgs += 1

		case *channeltypes.MsgTimeoutOnClose:
			response, err := ibcKeeper.TimeoutOnClose(sdk.WrapSDKContext(ctx), msg)
			if err != nil {
				return err
			}
			if response.Result == channeltypes.NOOP {
				redundancies += 1
			}
			packetMsgs += 1

		case *clienttypes.MsgUpdateClient:
			_, err := ibcKeeper.UpdateClient(sdk.WrapSDKContext(ctx), msg)
			if err != nil {
				return err
			}

		case *oracletypes.MsgAggregateExchangeRateVote:
			if ctx.IsReCheckTx() {
				continue
			}
			feederAddr, err := sdk.AccAddressFromBech32(msg.Feeder)
			if err != nil {
				return err
			}

			valAddr, err := sdk.ValAddressFromBech32(msg.Validator)
			if err != nil {
				return err
			}

			err = oracleKeeper.ValidateFeeder(ctx, feederAddr, valAddr)
			if err != nil {
				return err
			}

			if err := oracleKeeper.CheckAndSetSpamPreventionCounter(ctx, valAddr); err != nil {
				return err
			}
		}
	}

	// only return error if all packet messages are redundant
	if redundancies == packetMsgs && packetMsgs > 0 {
		return channeltypes.ErrRedundantTx
	}

	return nil
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
