module github.com/sei-protocol/sei-db/memiavl

go 1.19

require (
	cosmossdk.io/errors v1.0.0
	github.com/alitto/pond v1.8.3
	github.com/confio/ics23/go v0.9.0
	github.com/cosmos/cosmos-sdk v0.45.10
	github.com/cosmos/gogoproto v1.4.11
	github.com/cosmos/iavl v0.21.0-alpha.1.0.20230904092046-df3db2d96583
	github.com/ledgerwatch/erigon-lib v0.0.0-20230210071639-db0e7ed11263
	github.com/spf13/cast v1.5.0
	github.com/stretchr/testify v1.8.4
	github.com/tendermint/tm-db v0.6.8-0.20220519162814-e24b96538a12
	github.com/tidwall/btree v1.6.0
	github.com/tidwall/gjson v1.10.2
	github.com/tidwall/wal v1.1.7
	github.com/zbiljic/go-filelock v0.0.0-20170914061330-1dbf7103ab7d
	golang.org/x/exp v0.0.0-20230811145659-89c5cff77bcb
)

require (
	github.com/cespare/xxhash v1.1.0 // indirect
	github.com/cespare/xxhash/v2 v2.2.0 // indirect
	github.com/cosmos/gorocksdb v1.2.0 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/dgraph-io/badger/v2 v2.2007.4 // indirect
	github.com/dgraph-io/badger/v3 v3.2103.2 // indirect
	github.com/dgraph-io/ristretto v0.1.1 // indirect
	github.com/dgryski/go-farm v0.0.0-20200201041132-a6ae2369ad13 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/gogo/protobuf v1.3.3 // indirect
	github.com/golang/glog v1.1.0 // indirect
	github.com/golang/groupcache v0.0.0-20210331224755-41bb18bfe9da // indirect
	github.com/golang/protobuf v1.5.3 // indirect
	github.com/golang/snappy v0.0.4 // indirect
	github.com/google/btree v1.1.2 // indirect
	github.com/google/flatbuffers v1.12.1 // indirect
	github.com/google/go-cmp v0.5.9 // indirect
	github.com/jmhodges/levigo v1.0.0 // indirect
	github.com/klauspost/compress v1.16.3 // indirect
	github.com/kr/pretty v0.3.1 // indirect
	github.com/linxGnu/grocksdb v1.8.4 // indirect
	github.com/onsi/gomega v1.20.0 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/rogpeppe/go-internal v1.11.0 // indirect
	github.com/syndtr/goleveldb v1.0.1-0.20220721030215-126854af5e6d // indirect
	github.com/tendermint/tendermint v0.37.0-dev // indirect
	github.com/tidwall/match v1.1.1 // indirect
	github.com/tidwall/pretty v1.2.0 // indirect
	github.com/tidwall/tinylru v1.1.0 // indirect
	go.etcd.io/bbolt v1.3.7 // indirect
	go.opencensus.io v0.23.0 // indirect
	golang.org/x/crypto v0.12.0 // indirect
	golang.org/x/net v0.14.0 // indirect
	golang.org/x/sys v0.11.0 // indirect
	google.golang.org/protobuf v1.31.0 // indirect
	gopkg.in/check.v1 v1.0.0-20201130134442-10cb98267c6c // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace (
	github.com/cosmos/cosmos-sdk => github.com/sei-protocol/sei-cosmos v0.2.61-0.20230902071955-518544a18bab
	github.com/cosmos/iavl => github.com/sei-protocol/sei-iavl v0.1.8-0.20230726213826-031d03d26f2d
	github.com/gogo/protobuf => github.com/regen-network/protobuf v1.3.3-alpha.regen.1
	github.com/tendermint/tendermint => github.com/sei-protocol/sei-tendermint v0.2.28
	github.com/tendermint/tm-db => github.com/sei-protocol/tm-db v0.0.4
)
