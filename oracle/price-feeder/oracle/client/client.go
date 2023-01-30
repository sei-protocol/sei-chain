package client

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/rpc"
	"github.com/cosmos/cosmos-sdk/client/tx"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/cosmos/cosmos-sdk/simapp"
	simappparams "github.com/cosmos/cosmos-sdk/simapp/params"
	"github.com/cosmos/cosmos-sdk/telemetry"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/rs/zerolog"
	rpchttp "github.com/tendermint/tendermint/rpc/client/http"
	tmjsonclient "github.com/tendermint/tendermint/rpc/jsonrpc/client"
)

type (
	// OracleClient defines a structure that interfaces with the Umee node.
	OracleClient struct {
		Logger              zerolog.Logger
		ChainID             string
		KeyringBackend      string
		KeyringDir          string
		KeyringPass         string
		TMRPC               string
		RPCTimeout          time.Duration
		OracleAddr          sdk.AccAddress
		OracleAddrString    string
		ValidatorAddr       sdk.ValAddress
		ValidatorAddrString string
		FeeGranterAddr      sdk.AccAddress
		Encoding            simappparams.EncodingConfig
		GasPrices           string
		GasAdjustment       float64
		GRPCEndpoint        string
		KeyringPassphrase   string
		BlockHeightEvents   chan int64
	}

	passReader struct {
		pass string
		buf  *bytes.Buffer
	}
)

func NewOracleClient(
	ctx context.Context,
	logger zerolog.Logger,
	chainID string,
	keyringBackend string,
	keyringDir string,
	keyringPass string,
	tmRPC string,
	rpcTimeout time.Duration,
	oracleAddrString string,
	validatorAddrString string,
	feeGranterAddrString string,
	grpcEndpoint string,
	gasAdjustment float64,
	gasPrices string,
) (OracleClient, error) {
	oracleAddr, err := sdk.AccAddressFromBech32(oracleAddrString)
	if err != nil {
		return OracleClient{}, err
	}

	feegrantAddrErr, _ := sdk.AccAddressFromBech32(feeGranterAddrString)

	oracleClient := OracleClient{
		Logger:              logger.With().Str("module", "oracle_client").Logger(),
		ChainID:             chainID,
		KeyringBackend:      keyringBackend,
		KeyringDir:          keyringDir,
		KeyringPass:         keyringPass,
		TMRPC:               tmRPC,
		RPCTimeout:          rpcTimeout,
		OracleAddr:          oracleAddr,
		OracleAddrString:    oracleAddrString,
		ValidatorAddr:       sdk.ValAddress(validatorAddrString),
		ValidatorAddrString: validatorAddrString,
		FeeGranterAddr:      feegrantAddrErr,
		Encoding:            simapp.MakeTestEncodingConfig(),
		GasAdjustment:       gasAdjustment,
		GRPCEndpoint:        grpcEndpoint,
		GasPrices:           gasPrices,
		BlockHeightEvents:   make(chan int64, 1),
	}

	clientCtx, err := oracleClient.CreateClientContext()
	if err != nil {
		return OracleClient{}, err
	}

	blockHeight, err := rpc.GetChainHeight(clientCtx)
	if err != nil {
		return OracleClient{}, err
	}

	chainHeightUpdater := HeightUpdater{
		Logger:        logger,
		LastHeight:    blockHeight,
		ChBlockHeight: oracleClient.BlockHeightEvents,
	}

	err = chainHeightUpdater.Start(ctx, clientCtx.Client, oracleClient.Logger)
	if err != nil {
		return OracleClient{}, err
	}

	return oracleClient, nil
}

func newPassReader(pass string) io.Reader {
	return &passReader{
		pass: pass,
		buf:  new(bytes.Buffer),
	}
}

func (r *passReader) Read(p []byte) (n int, err error) {
	n, err = r.buf.Read(p)
	if err == io.EOF || n == 0 {
		r.buf.WriteString(r.pass + "\n")

		n, err = r.buf.Read(p)
	}

	return n, err
}

// BroadcastTx attempts to broadcast a signed transaction in best effort mode.
// Retry is not needed since we are doing this for every new block.
func (oc OracleClient) BroadcastTx(
	blockHeight int64,
	msgs ...sdk.Msg) error {

	clientCtx, err := oc.CreateClientContext()
	if err != nil {
		return err
	}
	txFactory, err := oc.CreateTxFactory()
	if err != nil {
		return err
	}

	resp, err := BroadcastTx(clientCtx, txFactory, oc.Logger, msgs...)

	if resp != nil && resp.Code != 0 {
		telemetry.IncrCounter(1, "failure", "tx", "code")
		err = fmt.Errorf("invalid response code from tx: %d", resp.Code)
		return err
	}

	if err != nil {
		var (
			code uint32
			hash string
		)
		if resp != nil {
			code = resp.Code
			hash = resp.TxHash
		}
		oc.Logger.Debug().
			Err(err).
			Int64("height", blockHeight).
			Str("tx_hash", hash).
			Uint32("tx_code", code).
			Msg("failed to broadcast tx; retrying...")
		return err
	}

	oc.Logger.Info().
		Uint32("tx_code", resp.Code).
		Str("tx_hash", resp.TxHash).
		Int64("tx_height", resp.Height).
		Msg(fmt.Sprintf("Successfully broadcasted tx at height %d", blockHeight))
	return nil
}

// CreateClientContext creates an SDK client Context instance used for transaction
// generation, signing and broadcasting.
func (oc OracleClient) CreateClientContext() (client.Context, error) {
	var keyringInput io.Reader
	if len(oc.KeyringPass) > 0 {
		keyringInput = newPassReader(oc.KeyringPass)
	} else {
		keyringInput = os.Stdin
	}

	kr, err := keyring.New("sei", oc.KeyringBackend, oc.KeyringDir, keyringInput)
	if err != nil {
		return client.Context{}, err
	}

	httpClient, err := tmjsonclient.DefaultHTTPClient(oc.TMRPC)
	if err != nil {
		return client.Context{}, err
	}

	httpClient.Timeout = oc.RPCTimeout

	tmRPC, err := rpchttp.NewWithClient(oc.TMRPC, httpClient)
	if err != nil {
		return client.Context{}, err
	}

	keyInfo, err := kr.KeyByAddress(oc.OracleAddr)
	if err != nil {
		return client.Context{}, err
	}

	clientCtx := client.Context{
		ChainID:           oc.ChainID,
		JSONCodec:         oc.Encoding.Marshaler,
		InterfaceRegistry: oc.Encoding.InterfaceRegistry,
		Output:            os.Stderr,
		BroadcastMode:     flags.BroadcastSync,
		TxConfig:          oc.Encoding.TxConfig,
		AccountRetriever:  authtypes.AccountRetriever{},
		Codec:             oc.Encoding.Marshaler,
		LegacyAmino:       oc.Encoding.Amino,
		Input:             os.Stdin,
		NodeURI:           oc.TMRPC,
		Client:            tmRPC,
		Keyring:           kr,
		FromAddress:       oc.OracleAddr,
		FromName:          keyInfo.GetName(),
		From:              keyInfo.GetName(),
		OutputFormat:      "json",
		UseLedger:         false,
		Simulate:          false,
		GenerateOnly:      false,
		Offline:           false,
		SkipConfirm:       true,
		FeeGranter:        oc.FeeGranterAddr,
	}

	return clientCtx, nil
}

// CreateTxFactory creates an SDK Factory instance used for transaction
// generation, signing and broadcasting.
func (oc OracleClient) CreateTxFactory() (tx.Factory, error) {
	clientCtx, err := oc.CreateClientContext()
	if err != nil {
		return tx.Factory{}, err
	}

	txFactory := tx.Factory{}.
		WithAccountRetriever(clientCtx.AccountRetriever).
		WithChainID(oc.ChainID).
		WithTxConfig(clientCtx.TxConfig).
		WithGasAdjustment(oc.GasAdjustment).
		WithGasPrices(oc.GasPrices).
		WithKeybase(clientCtx.Keyring).
		WithSignMode(signing.SignMode_SIGN_MODE_DIRECT).
		WithSimulateAndExecute(true)

	return txFactory, nil
}
