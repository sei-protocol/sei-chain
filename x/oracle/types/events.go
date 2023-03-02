package types

// Oracle module event types
const (
	EventTypeExchangeRateUpdate = "exchange_rate_update"
	EventTypeVote               = "vote"
	EventTypeFeedDelegate       = "feed_delegate"
	EventTypeAggregateVote      = "aggregate_vote"
	EventTypeEndSlashWindow     = "end_slash_window"

	AttributeKeyDenom         = "denom"
	AttributeKeyVoter         = "voter"
	AttributeKeyExchangeRate  = "exchange_rate"
	AttributeKeyExchangeRates = "exchange_rates"
	AttributeKeyOperator      = "operator"
	AttributeKeyFeeder        = "feeder"
	AttributeKeyMissCount     = "miss_count"
	AttributeKeyAbstainCount  = "abstain_count"
	AttributeKeyWinCount      = "win_count"
	AttributeKeySuccessCount  = "success_count"

	AttributeValueCategory = ModuleName
)
