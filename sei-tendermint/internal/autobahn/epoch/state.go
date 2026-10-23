package epoch

import (
	"errors"
	"fmt"

	dbproto "github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

const (
	StateModuleName = "autobahn_epoch"

	PrevEpochRecordKey    = "prev_epoch_record"
	CurrentEpochRecordKey = "current_epoch_record"
	NextEpochRecordKey    = "next_epoch_record"
)

// StateWindow is the epoch committee window committed to app state.
type StateWindow struct {
	Prev    *pb.EpochRecord
	Current *pb.EpochRecord
	Next    *pb.EpochRecord
}

// NewStateWindow returns the state representation for the three committees
// needed around current.
func NewStateWindow(
	prevIndex types.EpochIndex,
	prev *types.Committee,
	currentIndex types.EpochIndex,
	current *types.Committee,
	nextIndex types.EpochIndex,
	next *types.Committee,
) (StateWindow, error) {
	window := StateWindow{
		Prev:    encodeStateEpochRecord(prevIndex, prev),
		Current: encodeStateEpochRecord(currentIndex, current),
		Next:    encodeStateEpochRecord(nextIndex, next),
	}
	if err := window.Validate(); err != nil {
		return StateWindow{}, err
	}
	return window, nil
}

// ChangeSet encodes the window as one FlatKV named changeset.
func (w StateWindow) ChangeSet() (*dbproto.NamedChangeSet, error) {
	if err := w.Validate(); err != nil {
		return nil, err
	}
	return &dbproto.NamedChangeSet{
		Name: StateModuleName,
		Changeset: dbproto.ChangeSet{Pairs: []*dbproto.KVPair{
			stateKV(PrevEpochRecordKey, w.Prev),
			stateKV(CurrentEpochRecordKey, w.Current),
			stateKV(NextEpochRecordKey, w.Next),
		}},
	}, nil
}

// Validate checks that the three records form a consecutive epoch window.
func (w StateWindow) Validate() error {
	prev, _, err := DecodeStateEpochRecord(w.Prev)
	if err != nil {
		return fmt.Errorf("prev: %w", err)
	}
	current, _, err := DecodeStateEpochRecord(w.Current)
	if err != nil {
		return fmt.Errorf("current: %w", err)
	}
	next, _, err := DecodeStateEpochRecord(w.Next)
	if err != nil {
		return fmt.Errorf("next: %w", err)
	}
	if current != prev+1 {
		return fmt.Errorf("current epoch %d does not follow prev epoch %d", current, prev)
	}
	if next != current+1 {
		return fmt.Errorf("next epoch %d does not follow current epoch %d", next, current)
	}
	return nil
}

// DecodeStateWindow decodes the three values stored under the state-window keys.
func DecodeStateWindow(prev, current, next []byte) (StateWindow, error) {
	prevRecord, err := protoutils.Unmarshal[*pb.EpochRecord](prev)
	if err != nil {
		return StateWindow{}, fmt.Errorf("prev: %w", err)
	}
	currentRecord, err := protoutils.Unmarshal[*pb.EpochRecord](current)
	if err != nil {
		return StateWindow{}, fmt.Errorf("current: %w", err)
	}
	nextRecord, err := protoutils.Unmarshal[*pb.EpochRecord](next)
	if err != nil {
		return StateWindow{}, fmt.Errorf("next: %w", err)
	}
	window := StateWindow{
		Prev:    prevRecord,
		Current: currentRecord,
		Next:    nextRecord,
	}
	if err := window.Validate(); err != nil {
		return StateWindow{}, err
	}
	return window, nil
}

// DecodeStateEpochRecord decodes an epoch record stored in FlatKV state.
func DecodeStateEpochRecord(record *pb.EpochRecord) (types.EpochIndex, *types.Committee, error) {
	if record == nil {
		return 0, nil, errors.New("missing")
	}
	if record.Index == nil {
		return 0, nil, errors.New("index: missing")
	}
	idx := types.EpochIndex(*record.Index)
	committee, err := types.CommitteeConv.DecodeReq(record.Committee)
	if err != nil {
		return 0, nil, err
	}
	for lane := range committee.Lanes().All() {
		if lane.Joined > idx {
			return 0, nil, fmt.Errorf("member joined epoch %d after committee epoch %d", lane.Joined, idx)
		}
	}
	return idx, committee, nil
}

func encodeStateEpochRecord(idx types.EpochIndex, committee *types.Committee) *pb.EpochRecord {
	return &pb.EpochRecord{
		Index:     utils.Alloc(uint64(idx)),
		Committee: types.CommitteeConv.Encode(committee),
	}
}

func stateKV(key string, record *pb.EpochRecord) *dbproto.KVPair {
	return &dbproto.KVPair{
		Key:   []byte(key),
		Value: protoutils.Marshal(record),
	}
}
