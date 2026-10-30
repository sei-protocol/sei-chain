package epoch

import (
	"errors"
	"fmt"

	dbproto "github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils"
)

const (
	StateModuleName = "autobahn_epoch"

	PrevEpochRecordKey    = "prev_epoch_record"
	CurrentEpochRecordKey = "current_epoch_record"
	NextEpochRecordKey    = "next_epoch_record"
)

// EpochCommittee is the committee of one epoch.
type EpochCommittee struct {
	Index     types.EpochIndex
	Committee *types.Committee
}

// StateWindow is the window of three consecutive epoch committees committed to
// app state. Construct it with NewStateWindow or DecodeStateWindow.
type StateWindow struct {
	prev    EpochCommittee
	current EpochCommittee
	next    EpochCommittee
}

// NewStateWindow returns the window of the three consecutive epochs prev,
// current and next.
func NewStateWindow(
	prevIndex types.EpochIndex,
	prev *types.Committee,
	currentIndex types.EpochIndex,
	current *types.Committee,
	nextIndex types.EpochIndex,
	next *types.Committee,
) (StateWindow, error) {
	w := StateWindow{
		prev:    EpochCommittee{Index: prevIndex, Committee: prev},
		current: EpochCommittee{Index: currentIndex, Committee: current},
		next:    EpochCommittee{Index: nextIndex, Committee: next},
	}
	if err := w.validate(); err != nil {
		return StateWindow{}, err
	}
	return w, nil
}

// DecodeStateWindow decodes the three values stored under the state-window keys.
func DecodeStateWindow(prev, current, next []byte) (StateWindow, error) {
	var w StateWindow
	for _, f := range []struct {
		name  string
		raw   []byte
		epoch *EpochCommittee
	}{
		{"prev", prev, &w.prev},
		{"current", current, &w.current},
		{"next", next, &w.next},
	} {
		record, err := protoutils.Unmarshal[*pb.EpochRecord](f.raw)
		if err != nil {
			return StateWindow{}, fmt.Errorf("%s: %w", f.name, err)
		}
		idx, committee, err := decodeEpochRecord(record)
		if err != nil {
			return StateWindow{}, fmt.Errorf("%s: %w", f.name, err)
		}
		*f.epoch = EpochCommittee{Index: idx, Committee: committee}
	}
	if err := w.validate(); err != nil {
		return StateWindow{}, err
	}
	return w, nil
}

// Prev returns the epoch closed by the block that commits the window.
func (w StateWindow) Prev() EpochCommittee { return w.prev }

// Current returns the epoch that follows Prev.
func (w StateWindow) Current() EpochCommittee { return w.current }

// Next returns the epoch that follows Current.
func (w StateWindow) Next() EpochCommittee { return w.next }

// Equal reports whether both windows hold the same epochs and committees.
func (w StateWindow) Equal(other StateWindow) bool {
	return w.prev.equal(other.prev) && w.current.equal(other.current) && w.next.equal(other.next)
}

// ChangeSet encodes the window as one FlatKV named changeset.
func (w StateWindow) ChangeSet() *dbproto.NamedChangeSet {
	return &dbproto.NamedChangeSet{
		Name: StateModuleName,
		Changeset: dbproto.ChangeSet{Pairs: []*dbproto.KVPair{
			stateKV(PrevEpochRecordKey, w.prev),
			stateKV(CurrentEpochRecordKey, w.current),
			stateKV(NextEpochRecordKey, w.next),
		}},
	}
}

func (w StateWindow) validate() error {
	for _, f := range []struct {
		name  string
		epoch EpochCommittee
	}{{"prev", w.prev}, {"current", w.current}, {"next", w.next}} {
		if err := f.epoch.validate(); err != nil {
			return fmt.Errorf("%s: %w", f.name, err)
		}
	}
	if w.current.Index != w.prev.Index+1 {
		return fmt.Errorf("current epoch %d does not follow prev epoch %d", w.current.Index, w.prev.Index)
	}
	if w.next.Index != w.current.Index+1 {
		return fmt.Errorf("next epoch %d does not follow current epoch %d", w.next.Index, w.current.Index)
	}
	return nil
}

func (e EpochCommittee) validate() error {
	if e.Committee == nil {
		return errors.New("committee: missing")
	}
	return checkJoinedBy(e.Index, e.Committee)
}

func (e EpochCommittee) equal(other EpochCommittee) bool {
	return e.Index == other.Index && e.Committee.Equal(other.Committee)
}

func stateKV(key string, e EpochCommittee) *dbproto.KVPair {
	return &dbproto.KVPair{
		Key:   []byte(key),
		Value: protoutils.Marshal(encodeEpochRecord(e.Index, e.Committee)),
	}
}
