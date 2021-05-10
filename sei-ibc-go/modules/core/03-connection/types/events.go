package types

import (
	"fmt"

	host "github.com/cosmos/ibc-go/modules/core/24-host"
)

// IBC connection events
const (
	AttributeKeyConnectionID             = "connection_id"
	AttributeKeyClientID                 = "client_id"
	AttributeKeyCounterpartyClientID     = "counterparty_client_id"
	AttributeKeyCounterpartyConnectionID = "counterparty_connection_id"
)

// IBC connection events vars
var (
	EventTypeConnectionOpenInit    = "connection_open_init"
	EventTypeConnectionOpenTry     = "connection_open_try"
	EventTypeConnectionOpenAck     = "connection_open_ack"
	EventTypeConnectionOpenConfirm = "connection_open_confirm"

	AttributeValueCategory = fmt.Sprintf("%s_%s", host.ModuleName, SubModuleName)
)
