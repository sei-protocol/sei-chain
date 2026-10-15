package gigasim

import (
	"fmt"
	"time"

	"github.com/sei-protocol/sei-chain/giga/apphash/vault"
)

// How often awaitAppHash checks the hash vault's head.
const appHashPollInterval = 10 * time.Millisecond

// awaitAppHash waits until hashVault holds the app hash of block height.
func awaitAppHash(hashVault vault.HashVault, height uint64) error {
	deadline := time.Now().Add(hashWaitTimeout)
	for {
		head, err := hashVault.GetLatestBlock()
		if err != nil {
			return fmt.Errorf("failed to read the hash vault's head: %w", err)
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
