package gorocksdb

// #include <stdlib.h>
// #include "rocksdb/c.h"
import "C"
import (
	"errors"
	"unsafe"
)

// TransactionDB is a reusable handle to a RocksDB transactional database on disk, created by OpenTransactionDb.
type TransactionDB struct {
	c                 *C.rocksdb_transactiondb_t
	name              string
	opts              *Options
	transactionDBOpts *TransactionDBOptions
}

// OpenTransactionDb opens a database with the specified options.
func OpenTransactionDb(
	opts *Options,
	transactionDBOpts *TransactionDBOptions,
	name string,
) (*TransactionDB, error) {
	var (
		cErr  *C.char
		cName = C.CString(name)
	)
	defer C.free(unsafe.Pointer(cName))
	db := C.rocksdb_transactiondb_open(
		opts.c, transactionDBOpts.c, cName, &cErr)
	if cErr != nil {
		defer C.rocksdb_free(unsafe.Pointer(cErr))
		return nil, errors.New(C.GoString(cErr))
	}
	return &TransactionDB{
		name:              name,
		c:                 db,
		opts:              opts,
		transactionDBOpts: transactionDBOpts,
	}, nil
}

// NewSnapshot creates a new snapshot of the database.
func (db *TransactionDB) NewSnapshot() *Snapshot {
	return NewNativeSnapshot(C.rocksdb_transactiondb_create_snapshot(db.c))
}

// ReleaseSnapshot releases the snapshot and its resources.
func (db *TransactionDB) ReleaseSnapshot(snapshot *Snapshot) {
	C.rocksdb_transactiondb_release_snapshot(db.c, snapshot.c)
	snapshot.c = nil
}

// GetProperty returns the value of a database property.
func (db *TransactionDB) GetProperty(propName string) string {
	cprop := C.CString(propName)
	defer C.free(unsafe.Pointer(cprop))
	cValue := C.rocksdb_transactiondb_property_value(db.c, cprop)
	defer C.rocksdb_free(unsafe.Pointer(cValue))
	return C.GoString(cValue)
}

// TransactionBegin begins a new transaction
// with the WriteOptions and TransactionOptions given.
func (db *TransactionDB) TransactionBegin(
	opts *WriteOptions,
	transactionOpts *TransactionOptions,
	oldTransaction *Transaction,
) *Transaction {
	if oldTransaction != nil {
		return NewNativeTransaction(C.rocksdb_transaction_begin(
			db.c,
			opts.c,
			transactionOpts.c,
			oldTransaction.c,
		))
	}

	return NewNativeTransaction(C.rocksdb_transaction_begin(
		db.c, opts.c, transactionOpts.c, nil))
}

// Get returns the data associated with the key from the database.
func (db *TransactionDB) Get(opts *ReadOptions, key []byte) (*Slice, error) {
	var (
		cErr    *C.char
		cValLen C.size_t
		cKey    = byteToChar(key)
	)
	cValue := C.rocksdb_transactiondb_get(
		db.c, opts.c, cKey, C.size_t(len(key)), &cValLen, &cErr,
	)
	if cErr != nil {
		defer C.rocksdb_free(unsafe.Pointer(cErr))
		return nil, errors.New(C.GoString(cErr))
	}
	return NewSlice(cValue, cValLen), nil
}

// Put writes data associated with a key to the database.
func (db *TransactionDB) Put(opts *WriteOptions, key, value []byte) error {
	var (
		cErr   *C.char
		cKey   = byteToChar(key)
		cValue = byteToChar(value)
	)
	C.rocksdb_transactiondb_put(
		db.c, opts.c, cKey, C.size_t(len(key)), cValue, C.size_t(len(value)), &cErr,
	)
	if cErr != nil {
		defer C.rocksdb_free(unsafe.Pointer(cErr))
		return errors.New(C.GoString(cErr))
	}
	return nil
}

// Write writes a WriteBatch to the database
func (db *TransactionDB) Write(opts *WriteOptions, batch *WriteBatch) error {
	var cErr *C.char
	C.rocksdb_transactiondb_write(db.c, opts.c, batch.c, &cErr)
	if cErr != nil {
		defer C.rocksdb_free(unsafe.Pointer(cErr))
		return errors.New(C.GoString(cErr))
	}
	return nil
}

// Delete removes the data associated with the key from the database.
func (db *TransactionDB) Delete(opts *WriteOptions, key []byte) error {
	var (
		cErr *C.char
		cKey = byteToChar(key)
	)
	C.rocksdb_transactiondb_delete(db.c, opts.c, cKey, C.size_t(len(key)), &cErr)
	if cErr != nil {
		defer C.rocksdb_free(unsafe.Pointer(cErr))
		return errors.New(C.GoString(cErr))
	}
	return nil
}

// NewCheckpoint creates a new Checkpoint for this db.
func (db *TransactionDB) NewCheckpoint() (*Checkpoint, error) {
	var (
		cErr *C.char
	)
	cCheckpoint := C.rocksdb_transactiondb_checkpoint_object_create(
		db.c, &cErr,
	)
	if cErr != nil {
		defer C.rocksdb_free(unsafe.Pointer(cErr))
		return nil, errors.New(C.GoString(cErr))
	}

	return NewNativeCheckpoint(cCheckpoint), nil
}

// Close closes the database.
func (transactionDB *TransactionDB) Close() {
	C.rocksdb_transactiondb_close(transactionDB.c)
	transactionDB.c = nil
}

// OptimisticTransactionDB is a reusable handle to a RocksDB optimistic transactional database on disk, created by OpenOptimisticTransactionDb.
type OptimisticTransactionDB struct {
	c    *C.rocksdb_optimistictransactiondb_t
	name string
	opts *Options
}

// OpenTransactionDb opens a database with the specified options.
func OpenOptimisticTransactionDb(
	opts *Options,
	name string,
) (*OptimisticTransactionDB, error) {
	var (
		cErr  *C.char
		cName = C.CString(name)
	)
	defer C.free(unsafe.Pointer(cName))
	db := C.rocksdb_optimistictransactiondb_open(opts.c, cName, &cErr)
	if cErr != nil {
		defer C.rocksdb_free(unsafe.Pointer(cErr))
		return nil, errors.New(C.GoString(cErr))
	}
	return &OptimisticTransactionDB{
		name: name,
		c:    db,
		opts: opts,
	}, nil
}

// GetBaseDB returns the base database.
func (db *OptimisticTransactionDB) GetBaseDB() *DB {
	return &DB{
		c:      C.rocksdb_optimistictransactiondb_get_base_db(db.c),
		closer: func(c *C.rocksdb_t) { C.rocksdb_optimistictransactiondb_close_base_db(c) },
		name:   db.name,
		opts:   db.opts,
	}
}

// GetProperty returns the value of a database property.
func (db *OptimisticTransactionDB) GetProperty(propName string) string {
	cprop := C.CString(propName)
	defer C.free(unsafe.Pointer(cprop))
	cValue := C.rocksdb_optimistictransactiondb_property_value(db.c, cprop)
	defer C.rocksdb_free(unsafe.Pointer(cValue))
	return C.GoString(cValue)
}

// TransactionBegin begins a new transaction
// with the WriteOptions and TransactionOptions given.
func (db *OptimisticTransactionDB) TransactionBegin(
	opts *WriteOptions,
	transactionOpts *OptimisticTransactionOptions,
	oldTransaction *Transaction,
) *Transaction {
	if oldTransaction != nil {
		return NewNativeTransaction(C.rocksdb_optimistictransaction_begin(
			db.c,
			opts.c,
			transactionOpts.c,
			oldTransaction.c,
		))
	}

	return NewNativeTransaction(C.rocksdb_optimistictransaction_begin(
		db.c, opts.c, transactionOpts.c, nil))
}

// Write writes a WriteBatch to the database
func (db *OptimisticTransactionDB) Write(opts *WriteOptions, batch *WriteBatch) error {
	var cErr *C.char
	C.rocksdb_optimistictransactiondb_write(db.c, opts.c, batch.c, &cErr)
	if cErr != nil {
		defer C.rocksdb_free(unsafe.Pointer(cErr))
		return errors.New(C.GoString(cErr))
	}
	return nil
}

// Close closes the database.
func (transactionDB *OptimisticTransactionDB) Close() {
	C.rocksdb_optimistictransactiondb_close(transactionDB.c)
	transactionDB.c = nil
}

// NewCheckpoint creates a new Checkpoint for this db.
func (db *OptimisticTransactionDB) NewCheckpoint() (*Checkpoint, error) {
	var (
		cErr *C.char
	)
	cCheckpoint := C.rocksdb_optimistictransactiondb_checkpoint_object_create(
		db.c, &cErr,
	)
	if cErr != nil {
		defer C.rocksdb_free(unsafe.Pointer(cErr))
		return nil, errors.New(C.GoString(cErr))
	}

	return NewNativeCheckpoint(cCheckpoint), nil
}
