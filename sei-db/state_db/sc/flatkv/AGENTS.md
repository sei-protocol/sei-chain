# FlatKV

FlatKV is the Giga state commitment store. Its rows are part of consensus: the state hash is the
`stateHash` field of the Giga app hash, so every node must write the same bytes for the same state.

## State encoding

The FlatKV state is a set of rows. A row is a physical key, `module/inner_key`, and a serialized
value, a 9-byte header (version `0x00`, block height as big-endian `uint64`) followed by the data.
The state hash is the BLAKE3-256 checksum of the LtHash of all rows. The state sync archive
([`cmd/seid/cmd/statesync/FORMAT.md`](../../../../cmd/seid/cmd/statesync/FORMAT.md)) carries the rows
unchanged.

The code and its tests define each rule:

| Rule                                   | Defined in                                                    | Pinned by                                                        |
|----------------------------------------|---------------------------------------------------------------|------------------------------------------------------------------|
| Physical key                           | `ktype/ktype.go`                                              | `ktype/ktype_test.go`                                            |
| EVM kind bytes                         | `sei-db/common/keys/evm.go`                                   | `TestFormatVectors` (storage `0x03`, code `0x07`, account `0x0a` and one misc row) |
| Database of a row                      | `routePhysicalKey` in `store.go`                              | the round-trip tests in `import_export_test.go`                  |
| Values                                 | the schema comments in `vtype/*_data.go`                      | the `*GoldenFile_V0` tests on `vtype/testdata/*.hex`             |
| Metadata rows (`_meta/`), not state    | `ktype/meta.go`                                               | `ktype/metakey_test.go`; the importer refuses them               |
| Leaf, LtHash and state hash            | `lthash/lthash.go`, `lthash.SumDBHashes`, `LtHash.Checksum`   | `TestLtHashGoldenHashesUnchanged`, `TestBackendsAgreeWithReference` |
| All of the above, end to end           |                                                               | `TestFormatVectors` in `cmd/seid/cmd/statesync`                  |

## Changing an encoding rule

- A change to the key, value or hash rules changes the state hash. It needs a chain upgrade and a new
  state sync archive `format_version`.
- When `TestFormatVectorRowsUseTheFlatKVEncoders` fails, the encoders changed. Do not re-record the
  vectors of a released version to make it pass.
- The database of a row does not change the state hash, but reads use it to choose the value decoder.
  Moving rows between databases needs a migration of the local data, not a chain upgrade.
