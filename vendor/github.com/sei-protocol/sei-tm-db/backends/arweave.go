package backends

import (
	"encoding/json"
	"errors"
	"sort"

	"github.com/syndtr/goleveldb/leveldb"
	dbm "github.com/tendermint/tm-db"
)

const (
	IndexKeyPrefixLen = 128
	Sha256Base64Len   = 44
	IndexEntryLen     = IndexKeyPrefixLen + Sha256Base64Len
)

type IndexEntry struct {
	keyPrefix string
	txId      []byte
}

func NewIndexEntryFromBytes(bz []byte) IndexEntry {
	keyPrefix := string(bz[:IndexKeyPrefixLen])
	return IndexEntry{
		keyPrefix: keyPrefix,
		txId:      bz[IndexKeyPrefixLen:],
	}
}

// A read-only backend that stores data on Arweave. Each key being
// queried needs to be prefixed with 8 bytes indicating the version
// to query for, from an uint64 encoded in big endian format.
// A query is processed in 3 steps:
// 1. Get the Arweave transaction ID which stores the queried
//    version's index from a local leveldb
// 2. Query Arweave for the queried version's index and get the
//    transaction ID(s) which store the actual queried data
// 3. Query Arweave with the transaction ID(s) from 2 and find
//    the queried value.
//
// To use an iterator, both `start` and `end` need to have to same
// version prefix.
type ArweaveDB struct {
	txDataByIdGetter  func([]byte) ([]byte, error)
	versionTxIdGetter func([]byte) ([]byte, error)
	closer            func() error
}

var _ dbm.DB = (*ArweaveDB)(nil)

func NewArweaveDB(indexDBFullPath string, arweaveNodeURL string) (*ArweaveDB, error) {
	indexDB, err := leveldb.OpenFile(indexDBFullPath, nil)
	if err != nil {
		return nil, err
	}
	arweaveClient := NewClient(arweaveNodeURL)
	return &ArweaveDB{
		txDataByIdGetter: func(txId []byte) ([]byte, error) {
			return arweaveClient.DownloadChunkData(string(txId))
		},
		versionTxIdGetter: func(version []byte) ([]byte, error) {
			return indexDB.Get(version, nil)
		},
		closer: func() error {
			return indexDB.Close()
		},
	}, nil
}

func NewEmptyArweaveDB() *ArweaveDB {
	return &ArweaveDB{}
}

// Get implements DB.
func (db *ArweaveDB) Get(key []byte) ([]byte, error) {
	txIds, err := db.getArweaveTxIds(key)
	if err != nil {
		return nil, err
	}
	return db.getKeyByTxIds(key, txIds)
}

// Has implements DB.
func (db *ArweaveDB) Has(key []byte) (bool, error) {
	txIds, err := db.getArweaveTxIds(key)
	if err == nil {
		_, err = db.getKeyByTxIds(key, txIds)
	}
	if err != nil {
		if _, ok := err.(*ErrKeyNotFound); ok {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// Set implements DB.
func (db *ArweaveDB) Set(key []byte, value []byte) error {
	panic("Arweave backend is read-only")
}

// SetSync implements DB.
func (db *ArweaveDB) SetSync(key []byte, value []byte) error {
	panic("Arweave backend is read-only")
}

// Delete implements DB.
func (db *ArweaveDB) Delete(key []byte) error {
	panic("Arweave backend is read-only")
}

// DeleteSync implements DB.
func (db *ArweaveDB) DeleteSync(key []byte) error {
	panic("Arweave backend is read-only")
}

// Close implements DB.
func (db *ArweaveDB) Close() error {
	return db.closer()
}

// Print implements DB.
func (db *ArweaveDB) Print() error {
	return nil
}

// Stats implements DB.
func (db *ArweaveDB) Stats() map[string]string {
	stats := make(map[string]string)
	return stats
}

// NewBatch implements DB.
func (db *ArweaveDB) NewBatch() dbm.Batch {
	panic("Arweave backend is read-only")
}

// Iterator implements DB.
func (db *ArweaveDB) Iterator(start, end []byte) (dbm.Iterator, error) {
	return newArweaveDBIterator(start, end, db, false)
}

// ReverseIterator implements DB.
func (db *ArweaveDB) ReverseIterator(start, end []byte) (dbm.Iterator, error) {
	return newArweaveDBIterator(start, end, db, true)
}

func (db *ArweaveDB) getKeyByTxIds(key []byte, txIds [][]byte) ([]byte, error) {
	key = key[8:]
	for _, txId := range txIds {
		keyvalues, err := db.getTxDataAsMap(txId)
		if err != nil {
			return nil, err
		}
		if val, ok := keyvalues[string(key)]; ok {
			return []byte(val.(string)), nil
		}
	}
	return nil, &ErrKeyNotFound{string(key)}
}

func (db *ArweaveDB) getTxDataAsMap(txId []byte) (map[string]interface{}, error) {
	txData, err := db.txDataByIdGetter(txId)
	if err != nil {
		return nil, err
	}
	keyvalues := map[string]interface{}{}
	if err := json.Unmarshal(txData, &keyvalues); err != nil {
		return nil, err
	}
	return keyvalues, nil
}

// First 8 bytes of key is version in big endian.
// Since we take a constant sized (128 bytes) prefix as range in
// the index, it's possible for some hot prefixes to have multiple
// entries in the index, so we need to be able to return multiple
// Tx IDs here.
func (db *ArweaveDB) getArweaveTxIds(key []byte) ([][]byte, error) {
	version := key[:8]
	index, err := db.getIndex(version)
	if err != nil {
		return nil, err
	}
	keyString := string(key[8:])
	entries := getIndexEntries(keyString, index)
	res := [][]byte{}
	for _, entry := range entries {
		res = append(res, entry.txId)
	}
	return res, nil
}

func (db *ArweaveDB) getIndex(version []byte) ([]byte, error) {
	indexTxId, err := db.versionTxIdGetter(version)
	if err != nil {
		return nil, err
	}
	return db.txDataByIdGetter(indexTxId)
}

// TODO: change to binary search
func getIndexEntries(keyString string, index []byte) []IndexEntry {
	res := []IndexEntry{}
	for i := 0; i < len(index); i += IndexEntryLen {
		indexEntry := NewIndexEntryFromBytes(index[i : i+IndexEntryLen])
		if len(res) > 0 {
			if res[0].keyPrefix == indexEntry.keyPrefix {
				res = append(res, indexEntry)
			} else {
				break
			}
		} else if keyString <= indexEntry.keyPrefix {
			res = append(res, indexEntry)
		}
	}
	return res
}

func getIndexEntriesForRange(keyStart string, keyEnd string, index []byte) []IndexEntry {
	res := []IndexEntry{}
	reachedEnd := false
	for i := 0; i < len(index); i += IndexEntryLen {
		indexEntry := NewIndexEntryFromBytes(index[i : i+IndexEntryLen])
		if reachedEnd {
			if res[len(res)-1].keyPrefix == indexEntry.keyPrefix {
				res = append(res, indexEntry)
			} else {
				break
			}
		} else if keyStart <= indexEntry.keyPrefix {
			res = append(res, indexEntry)
		}
		if keyEnd <= indexEntry.keyPrefix {
			reachedEnd = true
		}
	}
	return res
}

type arweaveDBIterator struct {
	db      *ArweaveDB
	reverse bool

	start []byte
	end   []byte

	txIds             [][]byte
	currentTxData     map[string]interface{}
	currentSortedKeys []string
	currentKeyIdx     int
	txIdx             int

	finished bool
}

var _ dbm.Iterator = (*arweaveDBIterator)(nil)

func newArweaveDBIterator(start []byte, end []byte, db *ArweaveDB, reverse bool) (*arweaveDBIterator, error) {
	if string(start[:8]) != string(end[:8]) {
		return nil, errors.New("Start and end must be of the same version")
	}
	version := start[:8]
	index, err := db.getIndex(version)
	if err != nil {
		return nil, err
	}
	start, end = start[8:], end[8:]
	entries := getIndexEntriesForRange(string(start), string(end), index)
	txIds := [][]byte{}
	for _, entry := range entries {
		txIds = append(txIds, entry.txId)
	}
	txIdx := 0
	if reverse {
		txIdx = len(txIds) - 1
	}
	iter := &arweaveDBIterator{
		db:      db,
		reverse: reverse,
		start:   start,
		end:     end,
		txIds:   txIds,
		txIdx:   txIdx,
	}
	if err := iter.loadTx(); err != nil {
		return nil, err
	}
	if reverse {
		for string(iter.Key()) >= string(end) {
			iter.Next()
		}
	} else {
		for string(iter.Key()) < string(start) {
			iter.Next()
		}
	}
	return iter, nil
}

func (itr *arweaveDBIterator) loadTx() error {
	if itr.finished {
		return nil
	}
	if itr.txIdx >= len(itr.txIds) || itr.txIdx < 0 {
		itr.finished = true
		return nil
	}
	data, err := itr.db.getTxDataAsMap(itr.txIds[itr.txIdx])
	if err != nil {
		return err
	}
	if len(data) == 0 {
		if itr.reverse {
			itr.txIdx--
		} else {
			itr.txIdx++
		}
		return itr.loadTx()
	}
	itr.currentTxData = data
	itr.currentSortedKeys = make([]string, len(itr.currentTxData))
	i := 0
	for k := range itr.currentTxData {
		itr.currentSortedKeys[i] = k
		i++
	}
	sort.Strings(itr.currentSortedKeys)
	if itr.reverse {
		itr.currentKeyIdx = len(itr.currentSortedKeys) - 1
		if string(itr.Key()) < string(itr.start) {
			itr.finished = true
		}
	} else {
		itr.currentKeyIdx = 0
		if string(itr.Key()) >= string(itr.end) {
			itr.finished = true
		}
	}
	return nil
}

// Domain implements Iterator.
func (itr *arweaveDBIterator) Domain() ([]byte, []byte) {
	return []byte{}, []byte{}
}

// Valid implements Iterator.
func (itr *arweaveDBIterator) Valid() bool {
	return !itr.finished
}

// Key implements Iterator.
func (itr *arweaveDBIterator) Key() []byte {
	if itr.finished {
		return nil
	}
	return []byte(itr.currentSortedKeys[itr.currentKeyIdx])
}

// Value implements Iterator.
func (itr *arweaveDBIterator) Value() []byte {
	if itr.finished {
		return nil
	}
	return []byte(itr.currentTxData[itr.currentSortedKeys[itr.currentKeyIdx]].(string))
}

// Next implements Iterator.
func (itr *arweaveDBIterator) Next() {
	if !itr.Valid() {
		return
	}
	if itr.reverse {
		if itr.currentKeyIdx > 0 {
			itr.currentKeyIdx--
			if string(itr.Key()) < string(itr.start) {
				itr.finished = true
			}
		} else {
			itr.txIdx--
			if err := itr.loadTx(); err != nil {
				panic(err)
			}
		}
	} else {
		if itr.currentKeyIdx < len(itr.currentSortedKeys)-1 {
			itr.currentKeyIdx++
			if string(itr.Key()) >= string(itr.end) {
				itr.finished = true
			}
		} else {
			itr.txIdx++
			if err := itr.loadTx(); err != nil {
				panic(err)
			}
		}
	}
}

// Error implements Iterator.
func (itr *arweaveDBIterator) Error() error {
	return nil
}

// Close implements Iterator.
func (itr *arweaveDBIterator) Close() error {
	return nil
}
