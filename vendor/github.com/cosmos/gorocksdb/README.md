# gorocksdb, a Go wrapper for RocksDB

[![Build Status](https://travis-ci.org/cosmos/gorocksdb.svg)](https://travis-ci.org/tecbot/gorocksdb) [![GoDoc](https://godoc.org/github.com/tecbot/gorocksdb?status.svg)](http://godoc.org/github.com/tecbot/gorocksdb)

## Install

You'll need to build [RocksDB](https://github.com/facebook/rocksdb) v5.16+ on your machine.

After that, you can install gorocksdb using the following command:

    CGO_CFLAGS="-I/path/to/rocksdb/include" \
    CGO_LDFLAGS="-L/path/to/rocksdb -lrocksdb -lstdc++ -lm -lz -lbz2 -lsnappy -llz4 -lzstd" \
      go get github.com/cosmos/gorocksdb

Please note that this package might upgrade the required RocksDB version at any moment.
Vendoring is thus highly recommended if you require high stability.

*The [embedded CockroachDB RocksDB](https://github.com/cockroachdb/c-rocksdb) is no longer supported in gorocksdb.*


## Exact example of building a cosmos chain on a mac 


```bash
CGO_CFLAGS="-I/opt/homebrew/Cellar/rocksdb/6.27.3/include" \
CGO_LDFLAGS="-L/opt/homebrew/Cellar/rocksdb/6.27.3/lib -lrocksdb -lstdc++ -lm -lz -lbz2 -lsnappy -llz4 -lzstd -L/opt/homebrew/Cellar/snappy/1.1.9/lib -L/opt/homebrew/Cellar/lz4/1.9.3/lib/ -L /opt/homebrew/Cellar/zstd/1.5.0/lib/"  \
go install -tags rocksdb ./...
```
