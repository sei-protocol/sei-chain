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

// compactRecords drops records without entries and collapses records that
// share a changelog version (Kafka at-least-once redelivery) onto the latest
// one, so sinks write each version's rows once and version markers carry the
// newest offset. Order is preserved by the first occurrence of each version.
func compactRecords(records []Record) []Record {
	indexByVersion := make(map[int64]int, len(records))
	out := make([]Record, 0, len(records))
	for _, rec := range records {
		if rec.Entry == nil {
			continue
		}
		if idx, ok := indexByVersion[rec.Entry.Version]; ok {
			out[idx] = rec
			continue
		}
		indexByVersion[rec.Entry.Version] = len(out)
		out = append(out, rec)
	}
	return out
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

func entryMutationCapacity(entry *proto.ChangelogEntry) int {
	total := 0
	for _, changeset := range entry.Changesets {
		total += len(changeset.Changeset.Pairs)
	}
	return total
}
