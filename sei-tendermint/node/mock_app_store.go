package node

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/cockroachdb/pebble/v2"
	"github.com/ethereum/go-ethereum/common"

	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// mockAppStore persists the MockApp state a restarted node needs to resume after its last
// committed block: the height and app hash, the validator set, and every sender nonce.
// Writes skip fsync. A process restart keeps them; a host crash may lose the last few blocks,
// which the node then re-executes.
type mockAppStore struct {
	db *pebble.DB
}

var (
	mockAppTipKey          = []byte("tip")
	mockAppValidatorsKey   = []byte("validators")
	mockAppNonceKeyPrefix  = []byte("n")
	mockAppNonceKeyUpper   = []byte("o")
	errMockAppStoreCorrupt = errors.New("mock app store is corrupt")
)

func openMockAppStore(dir string) (*mockAppStore, error) {
	db, err := pebble.Open(dir, &pebble.Options{})
	if err != nil {
		return nil, fmt.Errorf("pebble.Open(%q): %w", dir, err)
	}
	return &mockAppStore{db: db}, nil
}

func (s *mockAppStore) Close() error { return s.db.Close() }

func mockAppNonceKey(addr common.Address) []byte {
	return append(append([]byte{}, mockAppNonceKeyPrefix...), addr.Bytes()...)
}

// load fills state from the store. It reports false when the store holds no committed block.
func (s *mockAppStore) load(state *mockAppState) (bool, error) {
	tip, closer, err := s.db.Get(mockAppTipKey)
	if errors.Is(err, pebble.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read tip: %w", err)
	}
	if len(tip) < 8 {
		_ = closer.Close()
		return false, fmt.Errorf("%w: tip is %d bytes", errMockAppStoreCorrupt, len(tip))
	}
	state.lastBlockHeight = int64(binary.BigEndian.Uint64(tip[:8])) //nolint:gosec // written from a non-negative height
	state.lastBlockAppHash = append([]byte{}, tip[8:]...)
	_ = closer.Close()

	raw, closer, err := s.db.Get(mockAppValidatorsKey)
	if err != nil {
		return false, fmt.Errorf("read validators: %w", err)
	}
	state.validators, err = decodeMockAppValidators(raw)
	_ = closer.Close()
	if err != nil {
		return false, err
	}

	iter, err := s.db.NewIter(&pebble.IterOptions{LowerBound: mockAppNonceKeyPrefix, UpperBound: mockAppNonceKeyUpper})
	if err != nil {
		return false, fmt.Errorf("open nonce iterator: %w", err)
	}
	for iter.First(); iter.Valid(); iter.Next() {
		key, value := iter.Key(), iter.Value()
		if len(key) != len(mockAppNonceKeyPrefix)+common.AddressLength || len(value) != 8 {
			_ = iter.Close()
			return false, fmt.Errorf("%w: nonce entry %x", errMockAppStoreCorrupt, key)
		}
		state.nextNonce[common.BytesToAddress(key[len(mockAppNonceKeyPrefix):])] = binary.BigEndian.Uint64(value)
	}
	if err := iter.Close(); err != nil {
		return false, fmt.Errorf("scan nonces: %w", err)
	}
	return true, nil
}

// mockAppSnapshot is what one save writes: the committed tip, the nonces that changed since the
// previous save, and the validator set when the store does not hold it yet.
type mockAppSnapshot struct {
	height     int64
	appHash    []byte
	nonces     map[common.Address]uint64
	validators utils.Option[[]abci.ValidatorUpdate]
}

// mergeNewer folds next, which was taken after s, into s. Nonces only grow, so the union with
// next's values is the state at next's height.
func (s *mockAppSnapshot) mergeNewer(next mockAppSnapshot) {
	s.height, s.appHash = next.height, next.appHash
	for addr, nonce := range next.nonces {
		s.nonces[addr] = nonce
	}
	if next.validators.IsPresent() {
		s.validators = next.validators
	}
}

// save writes snap in one batch.
func (s *mockAppStore) save(snap mockAppSnapshot) error {
	batch := s.db.NewBatch()
	defer func() { _ = batch.Close() }()
	tip := binary.BigEndian.AppendUint64(nil, uint64(snap.height)) //nolint:gosec // heights are non-negative
	if err := batch.Set(mockAppTipKey, append(tip, snap.appHash...), nil); err != nil {
		return err
	}
	if validators, ok := snap.validators.Get(); ok {
		raw, err := encodeMockAppValidators(validators)
		if err != nil {
			return err
		}
		if err := batch.Set(mockAppValidatorsKey, raw, nil); err != nil {
			return err
		}
	}
	for addr, nonce := range snap.nonces {
		if err := batch.Set(mockAppNonceKey(addr), binary.BigEndian.AppendUint64(nil, nonce), nil); err != nil {
			return err
		}
	}
	return batch.Commit(pebble.NoSync)
}

// mockAppSaver writes snapshots to the store on its own goroutine, so Commit does not wait for
// pebble. Snapshots that queue while a write runs merge into one. The store may therefore lag
// the committed tip by a few blocks, which a restarted node executes again.
type mockAppSaver struct {
	store   *mockAppStore
	pending utils.Mutex[*utils.Option[mockAppSnapshot]]
	wake    chan struct{}
	stop    chan struct{}
	stopped chan struct{}
}

func newMockAppSaver(store *mockAppStore) *mockAppSaver {
	saver := &mockAppSaver{
		store:   store,
		pending: utils.NewMutex(&utils.Option[mockAppSnapshot]{}),
		wake:    make(chan struct{}, 1),
		stop:    make(chan struct{}),
		stopped: make(chan struct{}),
	}
	go saver.run()
	return saver
}

// put queues snap behind any snapshot not yet written.
func (s *mockAppSaver) put(snap mockAppSnapshot) {
	for pending := range s.pending.Lock() {
		if queued, ok := pending.Get(); ok {
			queued.mergeNewer(snap)
			snap = queued
		}
		*pending = utils.Some(snap)
	}
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *mockAppSaver) run() {
	defer close(s.stopped)
	for {
		select {
		case <-s.wake:
			s.flush()
		case <-s.stop:
			s.flush()
			return
		}
	}
}

// flush writes the queued snapshot. A failed write goes back in front of anything queued since,
// so no nonce change is skipped.
func (s *mockAppSaver) flush() {
	var snap mockAppSnapshot
	var ok bool
	for pending := range s.pending.Lock() {
		snap, ok = pending.Get()
		*pending = utils.None[mockAppSnapshot]()
	}
	if !ok {
		return
	}
	if err := s.store.save(snap); err != nil {
		logger.Error("failed to save mock app state", "height", snap.height, "err", err)
		for pending := range s.pending.Lock() {
			if newer, ok := pending.Get(); ok {
				snap.mergeNewer(newer)
			}
			*pending = utils.Some(snap)
		}
	}
}

// Close writes the queued snapshot and closes the store.
func (s *mockAppSaver) Close() error {
	close(s.stop)
	<-s.stopped
	return s.store.Close()
}

func encodeMockAppValidators(validators []abci.ValidatorUpdate) ([]byte, error) {
	var out []byte
	for _, v := range validators {
		raw, err := v.Marshal()
		if err != nil {
			return nil, fmt.Errorf("marshal validator: %w", err)
		}
		out = binary.AppendUvarint(out, uint64(len(raw)))
		out = append(out, raw...)
	}
	return out, nil
}

func decodeMockAppValidators(raw []byte) ([]abci.ValidatorUpdate, error) {
	var validators []abci.ValidatorUpdate
	for len(raw) > 0 {
		n, size := binary.Uvarint(raw)
		if size <= 0 || uint64(len(raw)-size) < n {
			return nil, fmt.Errorf("%w: validator length", errMockAppStoreCorrupt)
		}
		var v abci.ValidatorUpdate
		if err := v.Unmarshal(raw[size : size+int(n)]); err != nil { //nolint:gosec // bounded by len(raw) above
			return nil, fmt.Errorf("%w: %w", errMockAppStoreCorrupt, err)
		}
		validators = append(validators, v)
		raw = raw[size+int(n):] //nolint:gosec // bounded by len(raw) above
	}
	return validators, nil
}
