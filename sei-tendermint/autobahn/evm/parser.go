package evm

import (
	"errors"
	"fmt"
	"runtime"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// minTxsPerHashWorker is the transaction count budgeted per hashing worker.
const minTxsPerHashWorker = 64

// Signer parses Ethereum transactions and optionally caches verified senders.
type Signer struct {
	signer  ethtypes.Signer
	workers int
	cache   utils.Option[*utils.Mutex[*cache[common.Hash, common.Address]]]
}

// NewSigner returns a transaction signer with the given cache capacity and worker count.
// A zero cache capacity disables caching; zero workers uses GOMAXPROCS.
func NewSigner(signer ethtypes.Signer, cacheCapacity, workers int) *Signer {
	if workers <= 0 {
		workers = runtime.GOMAXPROCS(0)
	}
	s := &Signer{signer: signer, workers: workers}
	if cacheCapacity > 0 {
		s.cache = utils.Some(new(utils.NewMutex(new(newCache[common.Hash, common.Address](cacheCapacity)))))
	}
	return s
}

// Remember caches the verified sender of tx when caching is enabled.
func (s *Signer) Remember(tx EvmTx) {
	if cached, ok := s.cache.Get(); ok {
		for senders := range cached.Lock() {
			senders.push(tx.Hash(), tx.Sender())
		}
	}
}

// ForgetTxs removes the senders of txs from the cache when caching is enabled.
func (s *Signer) ForgetTxs(txs []EvmTx) {
	if cached, ok := s.cache.Get(); ok {
		for senders := range cached.Lock() {
			for _, tx := range txs {
				senders.pop(tx.Hash())
			}
		}
	}
}

// ParseTx parses a replay-protected transaction and returns its verified sender.
func (s *Signer) ParseTx(raw RawTx) (EvmTxRaw, error) {
	tx := new(ethtypes.Transaction)
	if err := tx.UnmarshalBinary(raw); err != nil {
		return EvmTxRaw{}, err
	}
	if err := s.Admits(tx); err != nil {
		return EvmTxRaw{}, err
	}
	hash := tx.Hash()
	if cached, ok := s.cache.Get(); ok {
		for senders := range cached.Lock() {
			if sender, ok := senders.get(hash).Get(); ok {
				return EvmTxRaw{tx: tx, sender: sender, raw: raw}, nil
			}
		}
	}
	sender, err := ethtypes.Sender(s.signer, tx)
	if err != nil {
		return EvmTxRaw{}, err
	}
	return EvmTxRaw{tx: tx, sender: sender, raw: raw}, nil
}

// ParseTxs parses replay-protected transactions, reusing cached senders or recovering them.
// Results follow input order.
func (s *Signer) ParseTxs(raw []RawTx) ([]EvmTx, error) {
	if len(raw) == 0 {
		return []EvmTx{}, nil
	}
	workers := s.workers
	signer := s.signer
	cached, useCache := s.cache.Get()
	decoded := make([]*ethtypes.Transaction, len(raw))
	resolved := make([]EvmTx, len(raw))
	var missing []bool
	if useCache {
		missing = make([]bool, len(raw))
	}
	hashWorkers := min(workers, (len(raw)+minTxsPerHashWorker-1)/minTxsPerHashWorker)
	err := forEach(len(raw), hashWorkers, func(i int) error {
		tx := new(ethtypes.Transaction)
		if err := tx.UnmarshalBinary(raw[i]); err != nil {
			return err
		}
		if err := s.Admits(tx); err != nil {
			return err
		}
		tx.Hash()
		decoded[i] = tx
		return nil
	})
	if err != nil {
		return nil, err
	}
	missingCount := len(raw)
	if useCache {
		// Hashing, validation, allocation, and recovery stay outside the cache lock.
		for senders := range cached.Lock() {
			for i, tx := range decoded {
				if sender, ok := senders.get(tx.Hash()).Get(); ok {
					resolved[i] = EvmTx{tx: tx, sender: sender}
					missingCount--
				} else {
					missing[i] = true
				}
			}
		}
	}
	if missingCount == 0 {
		return resolved, nil
	}
	// Signature recovery remains parallel even for small batches of cache misses.
	err = forEach(len(raw), min(workers, missingCount), func(i int) error {
		if useCache && !missing[i] {
			return nil
		}
		sender, err := ethtypes.Sender(signer, decoded[i])
		if err != nil {
			return err
		}
		resolved[i] = EvmTx{tx: decoded[i], sender: sender}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return resolved, nil
}

// Admits checks replay protection, chain ID, and transaction-type support.
func (s *Signer) Admits(tx *ethtypes.Transaction) error {
	if !tx.Protected() {
		return errors.New("unprotected Ethereum transaction")
	}
	if tx.Type() == ethtypes.BlobTxType {
		return fmt.Errorf("unsupported transaction type: %w", ethtypes.ErrTxTypeNotSupported)
	}
	chainID := s.signer.ChainID()
	if chainID == nil || tx.ChainId().Cmp(chainID) != 0 {
		return fmt.Errorf("%w: expected %v, got %s", ethtypes.ErrInvalidChainId, chainID, tx.ChainId())
	}
	// SignatureValues checks transaction-type support before inspecting the signature.
	// A zero signature probes support without recovering the sender.
	var probe [crypto.SignatureLength]byte
	_, _, _, err := s.signer.SignatureValues(tx, probe[:])
	return err
}
