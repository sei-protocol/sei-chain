package node

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/cockroachdb/pebble/v2"
	"github.com/ethereum/go-ethereum/common"

	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
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

// save writes the committed tip, the validators when saveValidators is set, and the nonces of
// dirty senders, in one batch.
func (s *mockAppStore) save(state *mockAppState, dirty map[common.Address]struct{}, saveValidators bool) error {
	batch := s.db.NewBatch()
	defer func() { _ = batch.Close() }()
	tip := binary.BigEndian.AppendUint64(nil, uint64(state.lastBlockHeight)) //nolint:gosec // heights are non-negative
	if err := batch.Set(mockAppTipKey, append(tip, state.lastBlockAppHash...), nil); err != nil {
		return err
	}
	if saveValidators {
		raw, err := encodeMockAppValidators(state.validators)
		if err != nil {
			return err
		}
		if err := batch.Set(mockAppValidatorsKey, raw, nil); err != nil {
			return err
		}
	}
	for addr := range dirty {
		if err := batch.Set(mockAppNonceKey(addr), binary.BigEndian.AppendUint64(nil, state.nextNonce[addr]), nil); err != nil {
			return err
		}
	}
	return batch.Commit(pebble.NoSync)
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
