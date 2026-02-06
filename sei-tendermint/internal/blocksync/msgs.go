package blocksync

import (
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

const (
	BlockResponseMessagePrefixSize   = 4
	BlockResponseMessageFieldKeySize = 1
)

const (
	MaxMsgSize = types.MaxBlockSizeBytes +
		BlockResponseMessagePrefixSize +
		BlockResponseMessageFieldKeySize
)
