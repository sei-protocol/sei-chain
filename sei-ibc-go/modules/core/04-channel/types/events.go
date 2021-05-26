package types

import (
	"fmt"

	host "github.com/cosmos/ibc-go/modules/core/24-host"
)

// IBC channel events
const (
	AttributeKeyConnectionID       = "connection_id"
	AttributeKeyPortID             = "port_id"
	AttributeKeyChannelID          = "channel_id"
	AttributeCounterpartyPortID    = "counterparty_port_id"
	AttributeCounterpartyChannelID = "counterparty_channel_id"

	EventTypeSendPacket           = "send_packet"
	EventTypeRecvPacket           = "recv_packet"
	EventTypeWriteAck             = "write_acknowledgement"
	EventTypeAcknowledgePacket    = "acknowledge_packet"
	EventTypeTimeoutPacket        = "timeout_packet"
	EventTypeTimeoutPacketOnClose = "timeout_on_close_packet"

	// NOTE: DEPRECATED in favor of AttributeKeyDataHex
	AttributeKeyData = "packet_data"
	// NOTE: DEPRECATED in favor of AttributeKeyAckHex
	AttributeKeyAck = "packet_ack"

	AttributeKeyDataHex          = "packet_data_hex"
	AttributeKeyAckHex           = "packet_ack_hex"
	AttributeKeyTimeoutHeight    = "packet_timeout_height"
	AttributeKeyTimeoutTimestamp = "packet_timeout_timestamp"
	AttributeKeySequence         = "packet_sequence"
	AttributeKeySrcPort          = "packet_src_port"
	AttributeKeySrcChannel       = "packet_src_channel"
	AttributeKeyDstPort          = "packet_dst_port"
	AttributeKeyDstChannel       = "packet_dst_channel"
	AttributeKeyChannelOrdering  = "packet_channel_ordering"
	AttributeKeyConnection       = "packet_connection"
)

// IBC channel events vars
var (
	EventTypeChannelOpenInit     = "channel_open_init"
	EventTypeChannelOpenTry      = "channel_open_try"
	EventTypeChannelOpenAck      = "channel_open_ack"
	EventTypeChannelOpenConfirm  = "channel_open_confirm"
	EventTypeChannelCloseInit    = "channel_close_init"
	EventTypeChannelCloseConfirm = "channel_close_confirm"

	AttributeValueCategory = fmt.Sprintf("%s_%s", host.ModuleName, SubModuleName)
)
