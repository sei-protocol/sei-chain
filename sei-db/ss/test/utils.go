package sstest

import (
	"fmt"

	"github.com/cosmos/iavl"
	"github.com/sei-protocol/sei-db/proto"
	"github.com/sei-protocol/sei-db/ss/types"
)

// Fills the db with multiple keys each with different versions
// TODO: Return just changeset so it can be altered after return
func FillData(db types.StateStore, numKeys int, versions int) error {
	if numKeys <= 0 || versions <= 0 {
		panic("numKeys and versions must be greater than 0")
	}

	for i := int64(1); i < int64(versions+1); i++ {
		cs := &iavl.ChangeSet{}
		cs.Pairs = []*iavl.KVPair{}

		for j := 0; j < numKeys; j++ {
			key := fmt.Sprintf("key%03d", j)
			val := fmt.Sprintf("val%03d-%03d", j, i)
			cs.Pairs = append(cs.Pairs, &iavl.KVPair{Key: []byte(key), Value: []byte(val)})
		}

		ncs := &proto.NamedChangeSet{
			Name:      storeKey1,
			Changeset: *cs,
		}

		err := db.ApplyChangeset(i, ncs)
		if err != nil {
			return err
		}

	}

	return nil
}
