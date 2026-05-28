package consumer

import "github.com/sei-protocol/sei-chain/sei-db/proto"

type stateMutation struct {
	storeName string
	pair      *proto.KVPair
}

type stateMutationKey struct {
	storeName string
	key       string
}

func compactRecords(records []Record) []Record {
	for _, rec := range records {
		if rec.Entry == nil {
			out := make([]Record, 0, len(records))
			for _, rec := range records {
				if rec.Entry != nil {
					out = append(out, rec)
				}
			}
			return out
		}
	}
	return records
}

func compactMutations(entry *proto.ChangelogEntry) []stateMutation {
	if entry == nil {
		return nil
	}
	mutations := make([]stateMutation, 0, entryMutationCapacity(entry))
	indexByKey := make(map[stateMutationKey]int, cap(mutations))
	for _, ncs := range entry.Changesets {
		storeName := ncs.Name
		for _, pair := range ncs.Changeset.Pairs {
			key := stateMutationKey{storeName: storeName, key: string(pair.Key)}
			if idx, ok := indexByKey[key]; ok {
				mutations[idx].pair = pair
				continue
			}
			indexByKey[key] = len(mutations)
			mutations = append(mutations, stateMutation{storeName: storeName, pair: pair})
		}
	}
	return mutations
}

func mutationValue(pair *proto.KVPair) ([]byte, bool) {
	deleted := pair.Delete || pair.Value == nil
	if deleted {
		return nil, true
	}
	return pair.Value, false
}

func entryMutationCapacity(entry *proto.ChangelogEntry) int {
	total := 0
	for _, changeset := range entry.Changesets {
		total += len(changeset.Changeset.Pairs)
	}
	return total
}
