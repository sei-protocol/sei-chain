package types

// Oracle module event types
const (
	EventTypeExchangeRateUpdate = "exchange_rate_update"
	EventTypePrevote            = "prevote"
	EventTypeVote               = "vote"
	EventTypeFeedDelegate       = "feed_delegate"
	EventTypeAggregatePrevote   = "aggregate_prevote"
	EventTypeAggregateVote      = "aggregate_vote"

	AttributeKeyDenom         = "denom"
	AttributeKeyVoter         = "voter"
	AttributeKeyExchangeRate  = "exchange_rate"
	AttributeKeyExchangeRates = "exchange_rates"
	AttributeKeyOperator      = "operator"
	AttributeKeyFeeder        = "feeder"

	AttributeValueCategory = ModuleName
)
