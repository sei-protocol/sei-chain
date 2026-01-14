package store

import (
	"encoding/binary"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/samber/lo"
	"google.golang.org/protobuf/proto"

	"github.com/sei-protocol/sei-stream/storage/serde"
	"github.com/sei-protocol/sei-stream/storage/stores/kv"
	"github.com/sei-protocol/sei-stream/storage/types"
	"github.com/tendermint/tendermint/internal/autobahn/pkg/protocol"
	streamtypes "github.com/tendermint/tendermint/internal/autobahn/types"
)

// TipCutDBName is the name of the tipcut database directory.
const TipCutDBName = "tipcut.db"

// TipCutStore manages storage and caching of TipCut commits.
type TipCutStore struct {
	cache  kv.CacheKVStore[uint64, *streamtypes.CommitQC]
	db     types.DBStore
	logger zerolog.Logger
}

func tipCutStoreKeySerializer(k uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, k)
	return b
}

func tipCutStoreValueSerializer(v *streamtypes.CommitQC) []byte {
	return lo.Must(proto.Marshal(streamtypes.CommitQCConv.Encode(v)))
}

func tipCutStoreValueDeserializer(b []byte) (*streamtypes.CommitQC, error) {
	commit := &protocol.CommitQC{}
	if err := proto.Unmarshal(b, commit); err != nil {
		return nil, err
	}
	return streamtypes.CommitQCConv.Decode(commit)
}

// NewTipCutStore creates a new TipCutStore instance that manages TipCut commits
// using the provided database store.
func NewTipCutStore(
	db types.DBStore) *TipCutStore {
	return &TipCutStore{
		cache: *kv.NewCacheKVStore(
			db,
			serde.Serialization[uint64, *streamtypes.CommitQC]{
				KeySerializer:     tipCutStoreKeySerializer,
				ValueSerializer:   tipCutStoreValueSerializer,
				ValueDeserializer: tipCutStoreValueDeserializer,
			},
			false,
		),
		db:     db,
		logger: log.Logger,
	}
}

// Set inserts a CommitQC to the store.
func (t *TipCutStore) Set(qc *streamtypes.CommitQC) {
	t.cache.Set(uint64(qc.Proposal().Index()), qc)
}

// FlushToDB persists any cached changes for the given tipCutIndex to the underlying database.
// It returns an error if the database operation fails. The function logs information about
// the flush operation including the number of changes and latency.
func (t *TipCutStore) FlushToDB(tipCutIndex uint64) error {
	startTime := time.Now()
	kvPairs := t.cache.PopChangeset()

	if len(kvPairs) == 0 {
		return nil
	}

	changeset := types.Changeset{
		KVPairs: kvPairs,
		Version: tipCutIndex,
	}
	err := t.db.ApplyChangeset(changeset)
	if err != nil {
		return err
	}

	t.logger.Info().
		Int("changes", len(kvPairs)).
		Str("db", "tipcut.db").
		Uint64("tipCut", tipCutIndex).
		Dur("latency", time.Since(startTime)).
		Msg("Apply changesets")

	return nil
}
