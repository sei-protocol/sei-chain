package evmrpc

import (
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/core/bloombits"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
)

func Match(ctx sdk.Context, start uint64, end uint64, k *keeper.Keeper, filters [][][]byte) error {
	matcher := bloombits.NewMatcher(1, filters)
	matches := make(chan uint64, 64)

	session, err := matcher.Start(ctx.Context(), start, end, matches)
	if err != nil {
		return err
	}
	defer session.Close()

	bloomRequests := make(chan chan *bloombits.Retrieval)
	go func() {
		for {
			select {
			case request := <-bloomRequests:
				task := <-request
				task.Bitsets = make([][]byte, len(task.Sections))
				for i, section := range task.Sections {
					head := rawdb.ReadCanonicalHash(eth.chainDb, section)
					if bloom, err := rawdb.ReadBloomBits(eth.chainDb, task.Bit, section, head); err == nil {
						task.Bitsets[i] = bloom
					} else {
						task.Error = err
					}
				}
				request <- task
			}
		}
	}()
	go session.Multiplex(16, time.Duration(0), bloomRequests)

	return nil
}
