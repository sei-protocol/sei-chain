package params

import (
	srvconfig "github.com/cosmos/cosmos-sdk/server/config"
	"github.com/cosmos/cosmos-sdk/types/address"
	evmrpcconfig "github.com/sei-protocol/sei-chain/evmrpc/config"
	tmcfg "github.com/tendermint/tendermint/config"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

const (
	HumanCoinUnit = "sei"
	BaseCoinUnit  = "usei"
	UseiExponent  = 6

	DefaultBondDenom = BaseCoinUnit

	// Bech32PrefixAccAddr defines the Bech32 prefix of an account's address.
	Bech32PrefixAccAddr = "sei"
)

// UnsafeBypassCommitTimeoutOverride commits block as soon as we reach consensus instead of waiting
// for timeout, this may cause validators to not get their votes in time
var UnsafeBypassCommitTimeoutOverride = false

var (
	// Bech32PrefixAccPub defines the Bech32 prefix of an account's public key.
	Bech32PrefixAccPub = Bech32PrefixAccAddr + "pub"
	// Bech32PrefixValAddr defines the Bech32 prefix of a validator's operator address.
	Bech32PrefixValAddr = Bech32PrefixAccAddr + "valoper"
	// Bech32PrefixValPub defines the Bech32 prefix of a validator's operator public key.
	Bech32PrefixValPub = Bech32PrefixAccAddr + "valoperpub"
	// Bech32PrefixConsAddr defines the Bech32 prefix of a consensus node address.
	Bech32PrefixConsAddr = Bech32PrefixAccAddr + "valcons"
	// Bech32PrefixConsPub defines the Bech32 prefix of a consensus node public key.
	Bech32PrefixConsPub = Bech32PrefixAccAddr + "valconspub"
)

func init() {
	SetAddressPrefixes()
	RegisterDenoms()
}

func RegisterDenoms() {
	err := sdk.RegisterDenom(HumanCoinUnit, sdk.OneDec())
	if err != nil {
		panic(err)
	}
	err = sdk.RegisterDenom(BaseCoinUnit, sdk.NewDecWithPrec(1, UseiExponent))
	if err != nil {
		panic(err)
	}
}

func SetAddressPrefixes() {
	config := sdk.GetConfig()
	config.SetBech32PrefixForAccount(Bech32PrefixAccAddr, Bech32PrefixAccPub)
	config.SetBech32PrefixForValidator(Bech32PrefixValAddr, Bech32PrefixValPub)
	config.SetBech32PrefixForConsensusNode(Bech32PrefixConsAddr, Bech32PrefixConsPub)

	// This is copied from the cosmos sdk v0.43.0-beta1
	// source: https://github.com/cosmos/cosmos-sdk/blob/v0.43.0-beta1/types/address.go#L141
	config.SetAddressVerifier(func(bytes []byte) error {
		if len(bytes) == 0 {
			return sdkerrors.Wrap(sdkerrors.ErrUnknownAddress, "addresses cannot be empty")
		}

		if len(bytes) > address.MaxAddrLen {
			return sdkerrors.Wrapf(sdkerrors.ErrUnknownAddress, "address max length is %d, got %d", address.MaxAddrLen, len(bytes))
		}

		// TODO: Do we want to allow addresses of lengths other than 20 and 32 bytes?
		if len(bytes) != 20 && len(bytes) != 32 {
			return sdkerrors.Wrapf(sdkerrors.ErrUnknownAddress, "address length must be 20 or 32 bytes, got %d", len(bytes))
		}

		return nil
	})
}

// NodeMode represents the type of node being run
// Extends Tendermint's Mode with additional archive mode
type NodeMode string

const (
	// Reuse Tendermint's mode constants
	NodeModeValidator NodeMode = tmcfg.ModeValidator // "validator"
	NodeModeFull      NodeMode = tmcfg.ModeFull      // "full"
	NodeModeSeed      NodeMode = tmcfg.ModeSeed      // "seed"
	// Additional mode specific to Sei Chain
	NodeModeArchive NodeMode = "archive"
)

// IsFullnodeType returns true if the node is a fullnode-like node (full or archive)
func (m NodeMode) IsFullnodeType() bool {
	return m == NodeModeFull || m == NodeModeArchive
}

// setValidatorTypeTendermintConfig sets common Tendermint config for validator-like nodes
func setValidatorTypeTendermintConfig(config *tmcfg.Config) {
	config.TxIndex.Indexer = []string{"null"} // Validators don't need tx indexing
	config.P2P.AllowDuplicateIP = false
}

// setFullnodeTypeTendermintConfig sets common Tendermint config for fullnode-like nodes
func setFullnodeTypeTendermintConfig(config *tmcfg.Config) {
	config.TxIndex.Indexer = []string{"kv"} // Full nodes need tx indexing for queries
	config.RPC.ListenAddress = "tcp://0.0.0.0:26657"
	config.P2P.ListenAddress = "tcp://0.0.0.0:26656"
}

// SetTendermintConfigByMode sets Tendermint config values based on node mode
// Note: config.Mode should be set by the caller before calling this function
// Archive nodes should have config.Mode = "full" since Tendermint doesn't recognize "archive"
func SetTendermintConfigByMode(config *tmcfg.Config) {
	mode := NodeMode(config.Mode)

	switch mode {
	case NodeModeValidator:
		setValidatorTypeTendermintConfig(config)

	case NodeModeSeed:
		setValidatorTypeTendermintConfig(config)
		// Seed nodes need more connections to serve peers
		config.P2P.MaxConnections = 1000
		config.P2P.AllowDuplicateIP = true

	case NodeModeFull:
		setFullnodeTypeTendermintConfig(config)

	case NodeModeArchive:
		// Archive nodes use full node Tendermint config
		// The difference is in app config (keeping all history)
		setFullnodeTypeTendermintConfig(config)
	}
}

// setValidatorTypeAppConfig sets common app config for validator-like nodes
func setValidatorTypeAppConfig(config *srvconfig.Config) {
	// Services: validators should minimize exposed services for security
	config.API.Enable = false
	config.GRPC.Enable = false
	config.GRPCWeb.Enable = false
	config.StateStore.Enable = false
}

// setFullnodeTypeAppConfig sets common app config for fullnode-like nodes
func setFullnodeTypeAppConfig(config *srvconfig.Config) {
	// Services: full nodes provide query services
	config.API.Enable = true
	config.GRPC.Enable = true
	config.GRPCWeb.Enable = true
	config.StateStore.Enable = true

	// StateStore: full nodes keep recent history for queries
	config.StateStore.KeepRecent = 100000

	// MinRetainBlocks: prune Tendermint blocks older than 100k blocks
	config.MinRetainBlocks = 100000

	// Pruning uses defaults (nothing,0,0 - keep all state history)
}

// setArchiveTypeAppConfig configures archive node settings
// Archive nodes are like full nodes but keep all history
func setArchiveTypeAppConfig(config *srvconfig.Config) {
	// Start with full node configuration
	setFullnodeTypeAppConfig(config)

	// Archive nodes keep all history
	config.StateStore.KeepRecent = 0 // 0 = keep all state history
	config.MinRetainBlocks = 0       // 0 = keep all Tendermint blocks

	// Pruning uses defaults (nothing,0,0 - keep all state history)
}

// SetAppConfigByMode sets app config values based on node mode
func SetAppConfigByMode(config *srvconfig.Config, mode NodeMode) {
	switch mode {
	case NodeModeValidator, NodeModeSeed:
		// Validator and Seed nodes share the same app config
		setValidatorTypeAppConfig(config)

	case NodeModeFull:
		setFullnodeTypeAppConfig(config)

	case NodeModeArchive:
		setArchiveTypeAppConfig(config)

	default:
		// Default to full node settings
		SetAppConfigByMode(config, NodeModeFull)
	}
}

// SetEVMConfigByMode sets EVM config based on node mode
// Validators and seeds have EVM disabled, full nodes and archives have it enabled
func SetEVMConfigByMode(config *evmrpcconfig.Config, mode NodeMode) {
	evmEnabled := mode.IsFullnodeType()
	config.HTTPEnabled = evmEnabled
	config.WSEnabled = evmEnabled
}
