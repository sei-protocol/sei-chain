package types

import (
	"fmt"

	"github.com/tendermint/tendermint/crypto/tmhash"

	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// Oracle Errors
var (
	ErrInvalidExchangeRate   = sdkerrors.Register(ModuleName, 2, "invalid exchange rate")
	ErrNoVote                = sdkerrors.Register(ModuleName, 4, "no vote")
	ErrNoVotingPermission    = sdkerrors.Register(ModuleName, 5, "unauthorized voter")
	ErrInvalidHash           = sdkerrors.Register(ModuleName, 6, "invalid hash")
	ErrInvalidHashLength     = sdkerrors.Register(ModuleName, 7, fmt.Sprintf("invalid hash length; should equal %d", tmhash.TruncatedSize))
	ErrVerificationFailed    = sdkerrors.Register(ModuleName, 8, "hash verification failed")
	ErrNoAggregateVote       = sdkerrors.Register(ModuleName, 12, "no aggregate vote")
	ErrNoVoteTarget          = sdkerrors.Register(ModuleName, 13, "no vote target")
	ErrUnknownDenom          = sdkerrors.Register(ModuleName, 14, "unknown denom")
	ErrNoLatestPriceSnapshot = sdkerrors.Register(ModuleName, 15, "no latest snapshot")
	ErrInvalidTwapLookback   = sdkerrors.Register(ModuleName, 16, "Twap lookback seconds is greater than max lookback duration or less than or equal to 0")
	ErrNoTwapData            = sdkerrors.Register(ModuleName, 17, "No data for the twap calculation")
	ErrParsingOracleQuery    = sdkerrors.Register(ModuleName, 18, "Error parsing SeiOracleQuery")
	ErrGettingExchangeRates  = sdkerrors.Register(ModuleName, 19, "Error while getting Exchange Rates")
	ErrEncodingExchangeRates = sdkerrors.Register(ModuleName, 20, "Error encoding exchange rates as JSON")
	ErrGettingOralceTwaps    = sdkerrors.Register(ModuleName, 21, "Error while getting Oracle Twaps in wasmd")
	ErrEncodingOralceTwaps   = sdkerrors.Register(ModuleName, 22, "Error encoding oracle twaps as JSON")
)
