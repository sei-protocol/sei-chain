package receipt

PebbleDB backend -> current
Parquet backend

Interface: SS store
Impl: MVCC PebbleDB

1. Add interface for receipt store
2. Add PebbleDB backend receipt store impl (MVCC Pebble)
3. Refactor existing receipt SS store to use new receipt interface with PebbleDB backend

When designing receipt store interface:
	- Check how we write
	- Check how we read (getReceiptByHash, ethGetLog)
4. Add ReceiptCache impl
5. Add parquet backend impl


ReceiptStore interface -> User/App deal with
- Cache Layer
- DB layer
   - Pebble
   - Parquet
