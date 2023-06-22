package types

const (
	EventTypePlaceOrder          = "place_order"
	EventTypeCancelOrder         = "cancel_order"
	EventTypeDepositRent         = "deposit_rent"
	EventTypeRegisterContract    = "register_contract"
	EventTypeUnregisterContract  = "unregister_contract"
	EventTypeRegisterPair        = "register_pair"
	EventTypeSetQuantityTickSize = "set_quantity_tick_size"
	EventTypeSetPriceTickSize    = "set_price_tick_size"

	AttributeKeyOrderID         = "order_id"
	AttributeKeyCancellationID  = "cancellation_id"
	AttributeKeyContractAddress = "contract_address"
	AttributeKeyRentBalance     = "rent_balance"
	AttributeKeyPriceDenom      = "price_denom"
	AttributeKeyAssetDenom      = "asset_denom"

	AttributeValueCategory = ModuleName
)
