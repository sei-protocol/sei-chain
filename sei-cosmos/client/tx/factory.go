package tx

import (
	"errors"
	"fmt"
	"math"

	"github.com/spf13/pflag"

	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client/flags"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keyring"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/types/tx/signing"
)

// Factory defines a client transaction factory that facilitates generating and
// signing an application-specific transaction.
type Factory struct {
	keybase          keyring.Keyring
	txConfig         client.TxConfig
	accountRetriever client.AccountRetriever
	accountNumber    uint64
	sequence         uint64
	gas              uint64
	timeoutHeight    uint64
	chainID          string
	memo             string
	fees             sdk.Coins
	gasPrices        sdk.DecCoins
	signMode         signing.SignMode
}

// NewFactoryCLI creates a new Factory from the tx flags in flagSet. It returns an
// error if --gas is not an integer.
func NewFactoryCLI(clientCtx client.Context, flagSet *pflag.FlagSet) (Factory, error) {
	signModeStr := clientCtx.SignModeStr

	signMode := signing.SignMode_SIGN_MODE_UNSPECIFIED
	switch signModeStr {
	case flags.SignModeDirect:
		signMode = signing.SignMode_SIGN_MODE_DIRECT
	case flags.SignModeLegacyAminoJSON:
		signMode = signing.SignMode_SIGN_MODE_LEGACY_AMINO_JSON
	case flags.SignModeEIP191:
		signMode = signing.SignMode_SIGN_MODE_EIP_191
	}

	accNum, _ := flagSet.GetUint64(flags.FlagAccountNumber)
	accSeq, _ := flagSet.GetUint64(flags.FlagSequence)
	memo, _ := flagSet.GetString(flags.FlagNote)
	timeoutHeight, _ := flagSet.GetUint64(flags.FlagTimeoutHeight)

	gasStr, _ := flagSet.GetString(flags.FlagGas)
	gas, err := flags.ParseGasSetting(gasStr)
	if err != nil {
		return Factory{}, err
	}

	f := Factory{
		txConfig:         clientCtx.TxConfig,
		accountRetriever: clientCtx.AccountRetriever,
		keybase:          clientCtx.Keyring,
		chainID:          clientCtx.ChainID,
		gas:              gas,
		accountNumber:    accNum,
		sequence:         accSeq,
		timeoutHeight:    timeoutHeight,
		memo:             memo,
		signMode:         signMode,
	}

	feesStr, _ := flagSet.GetString(flags.FlagFees)
	f = f.WithFees(feesStr)

	gasPricesStr, _ := flagSet.GetString(flags.FlagGasPrices)
	f = f.WithGasPrices(gasPricesStr)

	return f, nil
}

func (f Factory) AccountNumber() uint64                     { return f.accountNumber }
func (f Factory) Sequence() uint64                          { return f.sequence }
func (f Factory) Gas() uint64                               { return f.gas }
func (f Factory) Keybase() keyring.Keyring                  { return f.keybase }
func (f Factory) ChainID() string                           { return f.chainID }
func (f Factory) Memo() string                              { return f.memo }
func (f Factory) Fees() sdk.Coins                           { return f.fees }
func (f Factory) GasPrices() sdk.DecCoins                   { return f.gasPrices }
func (f Factory) AccountRetriever() client.AccountRetriever { return f.accountRetriever }
func (f Factory) TimeoutHeight() uint64                     { return f.timeoutHeight }

// WithTxConfig returns a copy of the Factory with an updated TxConfig.
func (f Factory) WithTxConfig(g client.TxConfig) Factory {
	f.txConfig = g
	return f
}

// WithAccountRetriever returns a copy of the Factory with an updated AccountRetriever.
func (f Factory) WithAccountRetriever(ar client.AccountRetriever) Factory {
	f.accountRetriever = ar
	return f
}

// WithChainID returns a copy of the Factory with an updated chainID.
func (f Factory) WithChainID(chainID string) Factory {
	f.chainID = chainID
	return f
}

// WithGas returns a copy of the Factory with an updated gas value.
func (f Factory) WithGas(gas uint64) Factory {
	f.gas = gas
	return f
}

// WithFees returns a copy of the Factory with an updated fee.
func (f Factory) WithFees(fees string) Factory {
	parsedFees, err := sdk.ParseCoinsNormalized(fees)
	if err != nil {
		panic(err)
	}

	f.fees = parsedFees
	return f
}

// WithGasPrices returns a copy of the Factory with updated gas prices.
func (f Factory) WithGasPrices(gasPrices string) Factory {
	parsedGasPrices, err := sdk.ParseDecCoins(gasPrices)
	if err != nil {
		panic(err)
	}

	f.gasPrices = parsedGasPrices
	return f
}

// WithKeybase returns a copy of the Factory with updated Keybase.
func (f Factory) WithKeybase(keybase keyring.Keyring) Factory {
	f.keybase = keybase
	return f
}

// WithSequence returns a copy of the Factory with an updated sequence number.
func (f Factory) WithSequence(sequence uint64) Factory {
	f.sequence = sequence
	return f
}

// WithMemo returns a copy of the Factory with an updated memo.
func (f Factory) WithMemo(memo string) Factory {
	f.memo = memo
	return f
}

// WithAccountNumber returns a copy of the Factory with an updated account number.
func (f Factory) WithAccountNumber(accnum uint64) Factory {
	f.accountNumber = accnum
	return f
}

// SignMode returns the sign mode configured in the Factory
func (f Factory) SignMode() signing.SignMode {
	return f.signMode
}

// WithSignMode returns a copy of the Factory with an updated sign mode value.
func (f Factory) WithSignMode(mode signing.SignMode) Factory {
	f.signMode = mode
	return f
}

// WithTimeoutHeight returns a copy of the Factory with an updated timeout height.
func (f Factory) WithTimeoutHeight(height uint64) Factory {
	f.timeoutHeight = height
	return f
}

// BuildUnsignedTx builds a transaction to be signed given a set of messages.
// Once created, the fee, memo, and messages are set.
func (f Factory) BuildUnsignedTx(msgs ...sdk.Msg) (client.TxBuilder, error) {
	if f.chainID == "" {
		return nil, fmt.Errorf("chain ID required but not specified")
	}

	fees := f.fees

	if !f.gasPrices.IsZero() {
		if !fees.IsZero() {
			return nil, errors.New("cannot provide both fees and gas prices")
		}

		if f.gas > uint64(math.MaxInt64) {
			return nil, fmt.Errorf("gas %d exceeds max int64", f.gas)
		}
		glDec := sdk.NewDec(int64(f.gas)) //nolint:gosec // bounds checked above

		// Derive the fees based on the provided gas prices, where
		// fee = ceil(gasPrice * gasLimit).
		fees = make(sdk.Coins, len(f.gasPrices))

		for i, gp := range f.gasPrices {
			fee := gp.Amount.Mul(glDec)
			fees[i] = sdk.NewCoin(gp.Denom, fee.Ceil().RoundInt())
		}
	}

	tx := f.txConfig.NewTxBuilder()

	if err := tx.SetMsgs(msgs...); err != nil {
		return nil, err
	}

	tx.SetMemo(f.memo)
	tx.SetFeeAmount(fees)
	tx.SetGasLimit(f.gas)
	tx.SetTimeoutHeight(f.TimeoutHeight())

	return tx, nil
}

// PrintUnsignedTx will generate an unsigned transaction and print it to the writer
// specified by ctx.Output.
func (f Factory) PrintUnsignedTx(clientCtx client.Context, msgs ...sdk.Msg) error {
	unsignedTx, err := f.BuildUnsignedTx(msgs...)
	if err != nil {
		return err
	}

	json, err := clientCtx.TxConfig.TxJSONEncoder()(unsignedTx.GetTx())
	if err != nil {
		return err
	}

	return clientCtx.PrintString(fmt.Sprintf("%s\n", json))
}

// Prepare ensures the account defined by ctx.GetFromAddress() exists and
// if the account number and/or the account sequence number are zero (not set),
// they will be queried for and set on the provided Factory. A new Factory with
// the updated fields will be returned.
func (f Factory) Prepare(clientCtx client.Context) (Factory, error) {
	fc := f

	from := clientCtx.GetFromAddress()

	if err := fc.accountRetriever.EnsureExists(clientCtx, from); err != nil {
		return fc, err
	}

	initNum, initSeq := fc.accountNumber, fc.sequence
	if initNum == 0 || initSeq == 0 {
		num, seq, err := fc.accountRetriever.GetAccountNumberSequence(clientCtx, from)
		if err != nil {
			return fc, err
		}

		if initNum == 0 {
			fc = fc.WithAccountNumber(num)
		}

		if initSeq == 0 {
			fc = fc.WithSequence(seq)
		}
	}

	return fc, nil
}
