package cryptosim

import (
	"context"
	"fmt"
)

// A simulated reciept store.
type RecieptStoreSimulator struct {
	ctx    context.Context
	cancel context.CancelFunc

	config *CryptoSimConfig

	recieptsChan chan *block
}

// Creates a new reciept store simulator.
func NewRecieptStoreSimulator(
	ctx context.Context,
	config *CryptoSimConfig,
	recieptsChan chan *block,
) (*RecieptStoreSimulator, error) {
	r := &RecieptStoreSimulator{
		ctx:          ctx,
		config:       config,
		recieptsChan: recieptsChan,
	}
	go r.mainLoop()
	return r, nil
}

func (r *RecieptStoreSimulator) mainLoop() {
	for {
		select {
		case <-r.ctx.Done():
			// TODO add shutdown logic if needed
			return
		case blk := <-r.recieptsChan:
			r.processBlock(blk)
		}
	}
}

// Processes a block of reciepts.
func (r *RecieptStoreSimulator) processBlock(blk *block) {
	// TODO implement
	fmt.Printf("processing block %d with %d reciepts\n", blk.BlockNumber(), len(blk.reciepts)) // TODO remove print

	// for _, reciept := range blk.reciepts {
	// 	// TODO
	// }
}
