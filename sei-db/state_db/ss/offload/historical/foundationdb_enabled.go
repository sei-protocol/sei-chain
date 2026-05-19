//go:build foundationdb

package historical

import (
	"context"
	"errors"
	"fmt"

	"github.com/apple/foundationdb/bindings/go/src/fdb"
)

type FoundationDBClient struct {
	db     fdb.Database
	prefix string
	shards int
}

func OpenFoundationDBClient(cfg FoundationDBConfig) (*FoundationDBClient, error) {
	cfg.ApplyDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if err := selectFoundationDBAPIVersion(cfg.APIVersion); err != nil {
		return nil, err
	}
	db, err := fdb.OpenDatabase(cfg.ClusterFile)
	if err != nil {
		return nil, fmt.Errorf("open foundationdb: %w", err)
	}
	return &FoundationDBClient{db: db, prefix: cfg.Prefix, shards: cfg.Shards}, nil
}

func selectFoundationDBAPIVersion(version int) error {
	if selected, err := fdb.GetAPIVersion(); err == nil {
		if selected == version {
			return nil
		}
		return fmt.Errorf("foundationdb api version already selected as %d, requested %d", selected, version)
	}
	if err := fdb.APIVersion(version); err != nil {
		return fmt.Errorf("select foundationdb api version %d: %w", version, err)
	}
	return nil
}

func (c *FoundationDBClient) Close() error {
	c.db.Close()
	return nil
}

func (c *FoundationDBClient) WriteBatch(ctx context.Context, writes []FoundationDBWrite) error {
	if len(writes) == 0 {
		return nil
	}
	_, err := c.db.Transact(func(tr fdb.Transaction) (interface{}, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for _, write := range writes {
			if err := tr.Options().SetNextWriteNoWriteConflictRange(); err != nil {
				return nil, err
			}
			tr.Set(fdb.Key(write.Key), write.Value)
		}
		return nil, nil
	})
	if err != nil {
		return fmt.Errorf("foundationdb write batch: %w", err)
	}
	return nil
}

func (c *FoundationDBClient) LastVersion(ctx context.Context) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	ret, err := c.db.ReadTransact(func(rtr fdb.ReadTransaction) (interface{}, error) {
		type bucketRead struct {
			bucket int
			rows   fdb.RangeResult
		}
		reads := make([]bucketRead, 0, VersionBucketCount)
		for bucket := 0; bucket < VersionBucketCount; bucket++ {
			prefix := foundationDBVersionKeyPrefix(c.prefix, bucket)
			reads = append(reads, bucketRead{
				bucket: bucket,
				rows: rtr.GetRange(fdb.KeyRange{
					Begin: fdb.Key(prefix),
					End:   fdb.Key(foundationDBPrefixEnd(prefix)),
				}, fdb.RangeOptions{Limit: 1}),
			})
		}
		var maxVersion int64
		for _, read := range reads {
			kvs, err := read.rows.GetSliceWithError()
			if err != nil {
				return nil, fmt.Errorf("read latest foundationdb version bucket %d: %w", read.bucket, err)
			}
			if len(kvs) == 0 {
				continue
			}
			if version, ok := FoundationDBVersionFromKey(c.prefix, kvs[0].Key); ok && version > maxVersion {
				maxVersion = version
			}
		}
		return maxVersion, nil
	})
	if err != nil {
		return 0, err
	}
	return ret.(int64), nil
}

func (c *FoundationDBClient) Get(ctx context.Context, storeName string, key []byte, targetVersion int64) (Value, error) {
	if err := ctx.Err(); err != nil {
		return Value{}, err
	}
	ret, err := c.db.ReadTransact(func(rtr fdb.ReadTransaction) (interface{}, error) {
		prefix := FoundationDBMutationKeyPrefix(c.prefix, storeName, key, c.shards)
		start := FoundationDBMutationKey(c.prefix, storeName, key, targetVersion, c.shards)
		kvs, err := rtr.GetRange(fdb.KeyRange{
			Begin: fdb.Key(start),
			End:   fdb.Key(foundationDBPrefixEnd(prefix)),
		}, fdb.RangeOptions{Limit: 1}).GetSliceWithError()
		if err != nil {
			return Value{}, fmt.Errorf("foundationdb get lookup: %w", err)
		}
		if len(kvs) == 0 {
			return Value{}, ErrNotFound
		}
		return FoundationDBValueFromKeyValue(c.prefix, kvs[0].Key, kvs[0].Value)
	})
	if err != nil {
		return Value{}, err
	}
	return ret.(Value), nil
}

func (c *FoundationDBClient) BatchGet(ctx context.Context, targetVersion int64, lookups []Lookup) (map[Lookup]Value, error) {
	out := make(map[Lookup]Value, len(lookups))
	if len(lookups) == 0 {
		return out, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ret, err := c.db.ReadTransact(func(rtr fdb.ReadTransaction) (interface{}, error) {
		type lookupRead struct {
			lookup Lookup
			rows   fdb.RangeResult
		}
		reads := make([]lookupRead, 0, len(lookups))
		for _, lookup := range lookups {
			prefix := FoundationDBMutationKeyPrefix(c.prefix, lookup.StoreName, []byte(lookup.Key), c.shards)
			start := FoundationDBMutationKey(c.prefix, lookup.StoreName, []byte(lookup.Key), targetVersion, c.shards)
			reads = append(reads, lookupRead{
				lookup: lookup,
				rows: rtr.GetRange(fdb.KeyRange{
					Begin: fdb.Key(start),
					End:   fdb.Key(foundationDBPrefixEnd(prefix)),
				}, fdb.RangeOptions{Limit: 1}),
			})
		}
		out := make(map[Lookup]Value, len(lookups))
		for _, read := range reads {
			kvs, err := read.rows.GetSliceWithError()
			if err != nil {
				return nil, fmt.Errorf("foundationdb batch get lookup store=%s: %w", read.lookup.StoreName, err)
			}
			if len(kvs) == 0 {
				continue
			}
			value, err := FoundationDBValueFromKeyValue(c.prefix, kvs[0].Key, kvs[0].Value)
			if err != nil {
				if errors.Is(err, ErrNotFound) {
					continue
				}
				return nil, err
			}
			out[read.lookup] = value
		}
		return out, nil
	})
	if err != nil {
		return nil, err
	}
	return ret.(map[Lookup]Value), nil
}

func NewFoundationDBReader(cfg FoundationDBConfig) (Reader, error) {
	client, err := OpenFoundationDBClient(cfg)
	if err != nil {
		return nil, err
	}
	return &foundationDBReader{client: client}, nil
}

type foundationDBReader struct {
	client *FoundationDBClient
}

var _ Reader = (*foundationDBReader)(nil)

func (r *foundationDBReader) Close() error {
	return r.client.Close()
}

func (r *foundationDBReader) LastVersion(ctx context.Context) (int64, error) {
	return r.client.LastVersion(ctx)
}

func (r *foundationDBReader) Has(ctx context.Context, storeName string, key []byte, targetVersion int64) (bool, error) {
	_, err := r.Get(ctx, storeName, key, targetVersion)
	if err != nil {
		if err == ErrNotFound {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (r *foundationDBReader) Get(ctx context.Context, storeName string, key []byte, targetVersion int64) (Value, error) {
	return r.client.Get(ctx, storeName, key, targetVersion)
}

func (r *foundationDBReader) BatchGet(ctx context.Context, targetVersion int64, lookups []Lookup) (map[Lookup]Value, error) {
	return r.client.BatchGet(ctx, targetVersion, lookups)
}
