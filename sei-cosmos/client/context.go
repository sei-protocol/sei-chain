package client

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/spf13/viper"

	"gopkg.in/yaml.v2"

	"github.com/gogo/protobuf/proto"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	rpcclient "github.com/sei-protocol/sei-chain/sei-tendermint/rpc/client"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/client/local"

	"github.com/sei-protocol/sei-chain/sei-cosmos/codec"
	codectypes "github.com/sei-protocol/sei-chain/sei-cosmos/codec/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keyring"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
)

type Client = rpcclient.Client
type Context = ContextG[Client]
type LocalContext = ContextG[*local.Local]

type contextBase struct {
	FromAddress sdk.AccAddress
	ChainID     string
	// Deprecated: Codec codec will be changed to Codec: codec.Codec
	JSONCodec         codec.JSONCodec
	Codec             codec.Codec
	InterfaceRegistry codectypes.InterfaceRegistry
	Input             io.Reader
	Keyring           keyring.Keyring
	KeyringOptions    []keyring.Option
	Output            io.Writer
	OutputFormat      string
	Height            int64
	HomeDir           string
	KeyringDir        string
	From              string
	BroadcastMode     string
	FromName          string
	SignModeStr       string
	UseLedger         bool
	Simulate          bool
	GenerateOnly      bool
	PrintSignedOnly   bool
	Offline           bool
	SkipConfirm       bool
	TxConfig          TxConfig
	AccountRetriever  AccountRetriever
	NodeURI           string
	FeeGranter        sdk.AccAddress
	Viper             *viper.Viper

	// TODO: Deprecated (remove).
	LegacyAmino *codec.LegacyAmino
}

// Context implements a typical context created in SDK modules for transaction
// handling and queries.
type ContextG[C Client] struct {
	contextBase
	Client utils.Option[C]
}

func (ctx ContextG[C]) Any() Context {
	ctx2 := Context{contextBase: ctx.contextBase}
	if c, ok := ctx.Client.Get(); ok {
		ctx2.Client = utils.Some[Client](c)
	}
	return ctx2
}

// WithKeyring returns a copy of the context with an updated keyring.
func (ctx ContextG[C]) WithKeyring(k keyring.Keyring) ContextG[C] {
	ctx.Keyring = k
	return ctx
}

// WithKeyringOptions returns a copy of the context with an updated keyring.
func (ctx ContextG[C]) WithKeyringOptions(opts ...keyring.Option) ContextG[C] {
	ctx.KeyringOptions = opts
	return ctx
}

// WithInput returns a copy of the context with an updated input.
func (ctx ContextG[C]) WithInput(r io.Reader) ContextG[C] {
	// convert to a bufio.Reader to have a shared buffer between the keyring and the
	// the Commands, ensuring a read from one advance the read pointer for the other.
	// see https://github.com/cosmos/cosmos-sdk/issues/9566.
	ctx.Input = bufio.NewReader(r)
	return ctx
}

// Deprecated: WithJSONCodec returns a copy of the Context with an updated JSONCodec.
func (ctx ContextG[C]) WithJSONCodec(m codec.JSONCodec) ContextG[C] {
	ctx.JSONCodec = m
	// since we are using ctx.Codec everywhere in the SDK, for backward compatibility
	// we need to try to set it here as well.
	if c, ok := m.(codec.Codec); ok {
		ctx.Codec = c
	}
	return ctx
}

// WithCodec returns a copy of the Context with an updated Codec.
func (ctx ContextG[C]) WithCodec(m codec.Codec) ContextG[C] {
	ctx.JSONCodec = m
	ctx.Codec = m
	return ctx
}

// WithLegacyAmino returns a copy of the context with an updated LegacyAmino codec.
// TODO: Deprecated (remove).
func (ctx ContextG[C]) WithLegacyAmino(cdc *codec.LegacyAmino) ContextG[C] {
	ctx.LegacyAmino = cdc
	return ctx
}

// WithOutput returns a copy of the context with an updated output writer (e.g. stdout).
func (ctx ContextG[C]) WithOutput(w io.Writer) ContextG[C] {
	ctx.Output = w
	return ctx
}

// WithFrom returns a copy of the context with an updated from address or name.
func (ctx ContextG[C]) WithFrom(from string) ContextG[C] {
	ctx.From = from
	return ctx
}

// WithOutputFormat returns a copy of the context with an updated OutputFormat field.
func (ctx ContextG[C]) WithOutputFormat(format string) ContextG[C] {
	ctx.OutputFormat = format
	return ctx
}

// WithNodeURI returns a copy of the context with an updated node URI.
func (ctx ContextG[C]) WithNodeURI(nodeURI string) ContextG[C] {
	ctx.NodeURI = nodeURI
	return ctx
}

// WithHeight returns a copy of the context with an updated height.
func (ctx ContextG[C]) WithHeight(height int64) ContextG[C] {
	ctx.Height = height
	return ctx
}

// WithClient returns a copy of the context with an updated RPC client
// instance.
func (ctx ContextG[C]) WithClient(client C) ContextG[C] {
	return WithClient(ctx, client)
}

// WithClient returns a copy of the context with an updated RPC client
// instance.
func WithClient[C2, C1 Client](ctx ContextG[C1], client C2) ContextG[C2] {
	return ContextG[C2]{
		contextBase: ctx.contextBase,
		Client:      utils.Some(client),
	}
}

// WithUseLedger returns a copy of the context with an updated UseLedger flag.
func (ctx ContextG[C]) WithUseLedger(useLedger bool) ContextG[C] {
	ctx.UseLedger = useLedger
	return ctx
}

// WithChainID returns a copy of the context with an updated chain ID.
func (ctx ContextG[C]) WithChainID(chainID string) ContextG[C] {
	ctx.ChainID = chainID
	return ctx
}

// WithHomeDir returns a copy of the Context with HomeDir set.
func (ctx ContextG[C]) WithHomeDir(dir string) ContextG[C] {
	if dir != "" {
		ctx.HomeDir = dir
	}
	return ctx
}

// WithKeyringDir returns a copy of the Context with KeyringDir set.
func (ctx ContextG[C]) WithKeyringDir(dir string) ContextG[C] {
	ctx.KeyringDir = dir
	return ctx
}

// WithGenerateOnly returns a copy of the context with updated GenerateOnly value
func (ctx ContextG[C]) WithGenerateOnly(generateOnly bool) ContextG[C] {
	ctx.GenerateOnly = generateOnly
	return ctx
}

// WithSimulation returns a copy of the context with updated Simulate value
func (ctx ContextG[C]) WithSimulation(simulate bool) ContextG[C] {
	ctx.Simulate = simulate
	return ctx
}

// WithOffline returns a copy of the context with updated Offline value.
func (ctx ContextG[C]) WithOffline(offline bool) ContextG[C] {
	ctx.Offline = offline
	return ctx
}

// WithFromName returns a copy of the context with an updated from account name.
func (ctx ContextG[C]) WithFromName(name string) ContextG[C] {
	ctx.FromName = name
	return ctx
}

// WithFromAddress returns a copy of the context with an updated from account
// address.
func (ctx ContextG[C]) WithFromAddress(addr sdk.AccAddress) ContextG[C] {
	ctx.FromAddress = addr
	return ctx
}

// WithFeeGranterAddress returns a copy of the context with an updated fee granter account
// address.
func (ctx ContextG[C]) WithFeeGranterAddress(addr sdk.AccAddress) ContextG[C] {
	ctx.FeeGranter = addr
	return ctx
}

// WithBroadcastMode returns a copy of the context with an updated broadcast
// mode.
func (ctx ContextG[C]) WithBroadcastMode(mode string) ContextG[C] {
	ctx.BroadcastMode = mode
	return ctx
}

// WithSignModeStr returns a copy of the context with an updated SignMode
// value.
func (ctx ContextG[C]) WithSignModeStr(signModeStr string) ContextG[C] {
	ctx.SignModeStr = signModeStr
	return ctx
}

// WithSkipConfirmation returns a copy of the context with an updated SkipConfirm
// value.
func (ctx ContextG[C]) WithSkipConfirmation(skip bool) ContextG[C] {
	ctx.SkipConfirm = skip
	return ctx
}

// WithTxConfig returns the context with an updated TxConfig
func (ctx ContextG[C]) WithTxConfig(generator TxConfig) ContextG[C] {
	ctx.TxConfig = generator
	return ctx
}

// WithAccountRetriever returns the context with an updated AccountRetriever
func (ctx ContextG[C]) WithAccountRetriever(retriever AccountRetriever) ContextG[C] {
	ctx.AccountRetriever = retriever
	return ctx
}

// WithInterfaceRegistry returns the context with an updated InterfaceRegistry
func (ctx ContextG[C]) WithInterfaceRegistry(interfaceRegistry codectypes.InterfaceRegistry) ContextG[C] {
	ctx.InterfaceRegistry = interfaceRegistry
	return ctx
}

// WithViper returns the context with Viper field. This Viper instance is used to read
// client-side config from the config file.
func (ctx ContextG[C]) WithViper(prefix string) ContextG[C] {
	v := viper.New()
	v.SetEnvPrefix(prefix)
	v.AutomaticEnv()
	ctx.Viper = v
	return ctx
}

// PrintString prints the raw string to ctx.Output if it's defined, otherwise to os.Stdout
func (ctx ContextG[C]) PrintString(str string) error {
	return ctx.PrintBytes([]byte(str))
}

// PrintBytes prints the raw bytes to ctx.Output if it's defined, otherwise to os.Stdout.
// NOTE: for printing a complex state object, you should use ctx.PrintOutput
func (ctx ContextG[C]) PrintBytes(o []byte) error {
	writer := ctx.Output
	if writer == nil {
		writer = os.Stdout
	}

	_, err := writer.Write(o)
	return err
}

// PrintProto outputs toPrint to the ctx.Output based on ctx.OutputFormat which is
// either text or json. If text, toPrint will be YAML encoded. Otherwise, toPrint
// will be JSON encoded using ctx.Codec. An error is returned upon failure.
func (ctx ContextG[C]) PrintProto(toPrint proto.Message) error {
	// always serialize JSON initially because proto json can't be directly YAML encoded
	out, err := ctx.Codec.MarshalAsJSON(toPrint)
	if err != nil {
		return err
	}
	return ctx.printOutput(out)
}

// PrintObjectLegacy is a variant of PrintProto that doesn't require a proto.Message type
// and uses amino JSON encoding.
// Deprecated: It will be removed in the near future!
func (ctx ContextG[C]) PrintObjectLegacy(toPrint any) error {
	out, err := ctx.LegacyAmino.MarshalAsJSON(toPrint)
	if err != nil {
		return err
	}
	return ctx.printOutput(out)
}

func (ctx ContextG[C]) printOutput(out []byte) error {
	if ctx.OutputFormat == "text" {
		// handle text format by decoding and re-encoding JSON as YAML
		var j any

		err := json.Unmarshal(out, &j)
		if err != nil {
			return err
		}

		out, err = yaml.Marshal(j)
		if err != nil {
			return err
		}
	}

	writer := ctx.Output
	if writer == nil {
		writer = os.Stdout
	}

	_, err := writer.Write(out)
	if err != nil {
		return err
	}

	if ctx.OutputFormat != "text" {
		// append new-line for formats besides YAML
		_, err = writer.Write([]byte("\n"))
		if err != nil {
			return err
		}
	}

	return nil
}

// GetFromFields returns a from account address, account name and keyring type, given either an address or key name.
// If clientCtx.Simulate is true the keystore is not accessed and a valid address must be provided
// If clientCtx.GenerateOnly is true the keystore is only accessed if a key name is provided
func GetFromFields(clientCtx Context, kr keyring.Keyring, from string) (sdk.AccAddress, string, keyring.KeyType, error) {
	if from == "" {
		return nil, "", 0, nil
	}

	addr, err := sdk.AccAddressFromBech32(from)
	switch {
	case clientCtx.Simulate:
		if err != nil {
			return nil, "", 0, fmt.Errorf("a valid bech32 address must be provided in simulation mode: %w", err)
		}

		return addr, "", 0, nil

	case clientCtx.GenerateOnly:
		if err == nil {
			return addr, "", 0, nil
		}
	}

	var info keyring.Info
	if err == nil {
		info, err = kr.KeyByAddress(addr)
		if err != nil {
			return nil, "", 0, err
		}
	} else {
		info, err = kr.Key(from)
		if err != nil {
			return nil, "", 0, err
		}
	}

	return info.GetAddress(), info.GetName(), info.GetType(), nil
}

// NewKeyringFromBackend gets a Keyring object from a backend
func NewKeyringFromBackend(ctx Context, backend string) (keyring.Keyring, error) {
	if ctx.Simulate {
		return keyring.New(sdk.KeyringServiceName(), keyring.BackendMemory, ctx.KeyringDir, ctx.Input, ctx.KeyringOptions...)
	}

	return keyring.New(sdk.KeyringServiceName(), backend, ctx.KeyringDir, ctx.Input, ctx.KeyringOptions...)
}
