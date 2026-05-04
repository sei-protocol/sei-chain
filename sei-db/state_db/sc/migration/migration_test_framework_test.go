package migration

import (
	"context"
	"errors"

	ics23 "github.com/confio/ics23/go"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	dbm "github.com/tendermint/tm-db"
)

// TestInMemoryRouter is a [Router] backed by an in-memory map. The outer map keys
// are store (module) names and the inner map keys are store keys. It does not
// support iteration or proofs.
type TestInMemoryRouter struct {
	stores map[string]map[string][]byte
}

var _ Router = (*TestInMemoryRouter)(nil)

func NewTestInMemoryRouter() *TestInMemoryRouter {
	return &TestInMemoryRouter{stores: make(map[string]map[string][]byte)}
}

func (r *TestInMemoryRouter) Read(store string, key []byte) ([]byte, bool, error) {
	storeMap, ok := r.stores[store]
	if !ok {
		return nil, false, nil
	}
	value, ok := storeMap[string(key)]
	if !ok {
		return nil, false, nil
	}
	return value, true, nil
}

func (r *TestInMemoryRouter) ApplyChangeSets(_ context.Context, changesets []*proto.NamedChangeSet) error {
	for _, ncs := range changesets {
		if ncs == nil {
			continue
		}
		storeMap, ok := r.stores[ncs.Name]
		if !ok {
			storeMap = make(map[string][]byte)
			r.stores[ncs.Name] = storeMap
		}
		for _, pair := range ncs.Changeset.Pairs {
			if pair == nil {
				continue
			}
			if pair.Delete {
				delete(storeMap, string(pair.Key))
				continue
			}
			storeMap[string(pair.Key)] = append([]byte(nil), pair.Value...)
		}
	}
	return nil
}

func (r *TestInMemoryRouter) Iterator(store string, start []byte, end []byte, ascending bool) (dbm.Iterator, error) {
	return nil, errors.New("TestInMemoryRouter does not support iteration")
}

func (r *TestInMemoryRouter) GetProof(store string, key []byte) (*ics23.CommitmentProof, error) {
	return nil, errors.New("TestInMemoryRouter does not support proofs")
}
