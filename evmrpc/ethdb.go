package evmrpc

import (
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	rpcclient "github.com/tendermint/tendermint/rpc/client"
)

type ReadOnlyDb struct {
	tmClient rpcclient.Client
	k        *keeper.Keeper
}

func NewReadOnlyDb(tmClient rpcclient.Client, k *keeper.Keeper) ethdb.Database {
	return rawdb.NewDatabase(&ReadOnlyDb{tmClient: tmClient, k: k})
}

func (d *ReadOnlyDb) Has(key []byte) (bool, error) {
	panic("not implemented")
}

func (d *ReadOnlyDb) Get(key []byte) ([]byte, error) {
	panic("not implemented")
}

func (d *ReadOnlyDb) Put(key []byte, value []byte) error {
	panic("not implemented")
}

func (d *ReadOnlyDb) Delete(key []byte) error {
	panic("not implemented")
}

func (d *ReadOnlyDb) Stat(property string) (string, error) {
	panic("not implemented")
}

func (d *ReadOnlyDb) NewBatch() ethdb.Batch {
	panic("not implemented")
}

func (d *ReadOnlyDb) NewBatchWithSize(size int) ethdb.Batch {
	panic("not implemented")
}

func (d *ReadOnlyDb) NewIterator(prefix []byte, start []byte) ethdb.Iterator {
	panic("not implemented")
}

func (d *ReadOnlyDb) Compact(start []byte, limit []byte) error {
	panic("not implemented")
}

func (d *ReadOnlyDb) NewSnapshot() (ethdb.Snapshot, error) {
	panic("not implemented")
}

func (d *ReadOnlyDb) Close() error {
	panic("not implemented")
}
