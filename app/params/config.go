package params

import (
	"time"

	"github.com/cosmos/cosmos-sdk/types/address"
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

func SetTendermintConfigs(config *tmcfg.Config) {
	// P2P configs
	config.P2P.MaxConnections = 200
	config.P2P.SendRate = 20480000
	config.P2P.RecvRate = 20480000
	config.P2P.MaxPacketMsgPayloadSize = 1000000 // 1MB
	config.P2P.FlushThrottleTimeout = 10 * time.Millisecond
	// Mempool configs
	config.Mempool.Size = 1000
	config.Mempool.MaxTxsBytes = 10737418240
	config.Mempool.MaxTxBytes = 2048576
	config.Mempool.TTLDuration = 5 * time.Second
	config.Mempool.TTLNumBlocks = 10
	// Consensus Configs
	config.Consensus.GossipTransactionKeyOnly = true
	config.Consensus.UnsafeProposeTimeoutOverride = 300 * time.Millisecond
	config.Consensus.UnsafeProposeTimeoutDeltaOverride = 50 * time.Millisecond
	config.Consensus.UnsafeVoteTimeoutOverride = 50 * time.Millisecond
	config.Consensus.UnsafeVoteTimeoutDeltaOverride = 50 * time.Millisecond
	config.Consensus.UnsafeCommitTimeoutOverride = 200 * time.Millisecond
	config.Consensus.UnsafeBypassCommitTimeoutOverride = &UnsafeBypassCommitTimeoutOverride
	// Metrics
	config.Instrumentation.Prometheus = true
}
