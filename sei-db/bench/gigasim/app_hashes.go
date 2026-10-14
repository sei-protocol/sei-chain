package gigasim

import (
	"fmt"
	"time"

	"github.com/sei-protocol/sei-chain/giga/apphash/builder"
)

// How often awaitAppHash checks the app hash builder's head.
const appHashPollInterval = 10 * time.Millisecond

// awaitAppHash waits until appHashBuilder has stored the app hash of block height.
func awaitAppHash(appHashBuilder builder.AppHashBuilder, height uint64) error {
	deadline := time.Now().Add(hashWaitTimeout)
	for {
		head, err := appHashBuilder.GetLatestBlock()
		if err != nil {
			return fmt.Errorf("failed to read the app hash builder's head: %w", err)
		}
		if head >= height {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("the app hash of block %d was not stored within %s", height, hashWaitTimeout)
		}
		time.Sleep(appHashPollInterval)
	}
}
