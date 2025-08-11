package blocksync

import (
	bcproto "github.com/sei-protocol/sei-chain/tendermint/proto/tendermint/blocksync"
	"github.com/sei-protocol/sei-chain/tendermint/types"
)

const (
	MaxMsgSize = types.MaxBlockSizeBytes +
		bcproto.BlockResponseMessagePrefixSize +
		bcproto.BlockResponseMessageFieldKeySize
)
