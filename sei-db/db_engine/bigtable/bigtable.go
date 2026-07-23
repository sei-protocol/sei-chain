// Package bigtable is a thin Bigtable data-plane client used as an MVCC
// history store: row-key encoding, ReadRows chunk assembly, MutateRows result
// accounting, bounded read retries, and per-RPC cost metrics.
package bigtable

import (
	"context"
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"io"
	"regexp"
	"strings"
	"time"

	"cloud.google.com/go/bigtable/apiv2/bigtablepb"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/oauth"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/status"
)

const (
	DefaultFamily = "state"
	DefaultShards = 256

	mutationPrefix = byte('m')
	versionPrefix  = byte('v')
	upgradePrefix  = byte('u')

	ValueColumn   = "value"
	DeletedColumn = "deleted"

	// VersionBucketCount spreads monotonically increasing block-version markers
	// across a bounded set of row prefixes while keeping LastVersion cheap.
	VersionBucketCount = 64
)

// VersionBucket maps a version to its marker bucket.
func VersionBucket(version int64) int {
	if version < 0 {
		version = -version
	}
	return int(version % VersionBucketCount)
}

const (
	bigtableEndpoint = "bigtable.googleapis.com:443"

	defaultReadWorkers = 16

	// Transient stream failures (tablet moves, GFE restarts) surface as
	// UNAVAILABLE; retry reads a bounded number of times with backoff.
	readAttempts     = 3
	readRetryBackoff = 100 * time.Millisecond

	maxUint16Int   = 1<<16 - 1
	maxUint32Int   = 1<<32 - 1
	maxInt64Uint64 = 1<<63 - 1
)

// Client speaks the Bigtable data protocol directly over gRPC. The official
// cloud.google.com/go/bigtable client requires a newer google.golang.org/grpc
// than the repo-wide v1.57 replace pin allows, so this package carries its
// own thin transport: ReadRows chunk assembly, MutateRows result accounting,
// bounded read retries, and per-RPC cost metrics.
type Client struct {
	conn       *grpc.ClientConn
	data       bigtablepb.BigtableClient
	tableName  string
	appProfile string
	// table is the short table name used as the metric attribute.
	table   string
	metrics *bigtableMetrics
}

type Cell struct {
	Family    string
	Qualifier string
	Value     []byte
}

type Row struct {
	Key   string
	Cells []Cell
}

type SetCell struct {
	Family          string
	Qualifier       string
	TimestampMicros int64
	Value           []byte
}

type RowMutation struct {
	RowKey   string
	SetCells []SetCell
}

type Config struct {
	ProjectID  string
	InstanceID string
	Table      string
	Family     string
	AppProfile string
	Shards     int
}

func (c *Config) ApplyDefaults() {
	if c.Family == "" {
		c.Family = DefaultFamily
	}
	if c.Shards == 0 {
		c.Shards = DefaultShards
	}
}

// Configured reports whether all required connection parameters are set.
func (c Config) Configured() bool {
	return strings.TrimSpace(c.ProjectID) != "" &&
		strings.TrimSpace(c.InstanceID) != "" &&
		strings.TrimSpace(c.Table) != ""
}

func (c *Config) Validate() error {
	if strings.TrimSpace(c.ProjectID) == "" {
		return fmt.Errorf("bigtable project id is required")
	}
	if strings.TrimSpace(c.InstanceID) == "" {
		return fmt.Errorf("bigtable instance id is required")
	}
	if strings.TrimSpace(c.Table) == "" {
		return fmt.Errorf("bigtable table is required")
	}
	if strings.TrimSpace(c.Family) == "" {
		return fmt.Errorf("bigtable family is required")
	}
	if c.Shards <= 0 || c.Shards > maxUint16Int {
		return fmt.Errorf("bigtable shards must be between 1 and 65535")
	}
	return nil
}

func NewClient(ctx context.Context, cfg Config) (*Client, error) {
	cfg.ApplyDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	perRPC, err := oauth.NewApplicationDefault(ctx, "https://www.googleapis.com/auth/bigtable.data", "https://www.googleapis.com/auth/cloud-platform")
	if err != nil {
		return nil, fmt.Errorf("bigtable auth: %w", err)
	}
	conn, err := grpc.DialContext(ctx, bigtableEndpoint,
		grpc.WithTransportCredentials(credentials.NewClientTLSFromCert(nil, "")),
		grpc.WithPerRPCCredentials(perRPC),
		// Detect silently dropped connections instead of hanging reads on the
		// OS TCP timeout.
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:    30 * time.Second,
			Timeout: 10 * time.Second,
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("open bigtable connection: %w", err)
	}
	return &Client{
		conn:       conn,
		data:       bigtablepb.NewBigtableClient(conn),
		tableName:  fullTableName(cfg.ProjectID, cfg.InstanceID, cfg.Table),
		appProfile: cfg.AppProfile,
		table:      cfg.Table,
		metrics:    newBigtableMetrics(),
	}, nil
}

type ReadRowsFunc func(ctx context.Context, startKey, endKey []byte, limit int64, family string, f func(Row) bool, qualifiers ...string) error

type ApplyBulkFunc func(ctx context.Context, rows []RowMutation) ([]error, error)

func (c *Client) Close() error {
	if c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

// ReadRows retries transient stream failures as long as no row has been
// delivered to the callback yet, which covers every limit-1 point read and
// version-bucket scan in this package.
func (c *Client) ReadRows(ctx context.Context, startKey, endKey []byte, limit int64, family string, f func(Row) bool, qualifiers ...string) error {
	return readRowsWithRetry(ctx, c.readRowsOnce, startKey, endKey, limit, family, f, qualifiers...)
}

func readRowsWithRetry(ctx context.Context, once ReadRowsFunc, startKey, endKey []byte, limit int64, family string, f func(Row) bool, qualifiers ...string) error {
	backoff := readRetryBackoff
	var delivered bool
	for attempt := 1; ; attempt++ {
		err := once(ctx, startKey, endKey, limit, family, func(row Row) bool {
			delivered = true
			return f(row)
		}, qualifiers...)
		if err == nil || delivered || attempt >= readAttempts || status.Code(err) != codes.Unavailable {
			return err
		}
		if sleepErr := sleepWithContext(ctx, backoff); sleepErr != nil {
			return err
		}
		backoff *= 2
	}
}

func sleepWithContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *Client) readRowsOnce(ctx context.Context, startKey, endKey []byte, limit int64, family string, f func(Row) bool, qualifiers ...string) error {
	start := time.Now()
	var rowsRead, bytesRead int64
	defer func() {
		c.metrics.recordRead(ctx, c.table, time.Since(start), rowsRead, bytesRead)
	}()
	req := &bigtablepb.ReadRowsRequest{
		TableName:    c.tableName,
		AppProfileId: c.appProfile,
		Rows: &bigtablepb.RowSet{RowRanges: []*bigtablepb.RowRange{{
			StartKey: &bigtablepb.RowRange_StartKeyClosed{StartKeyClosed: startKey},
			EndKey:   &bigtablepb.RowRange_EndKeyOpen{EndKeyOpen: endKey},
		}}},
		RowsLimit: limit,
	}
	if len(endKey) == 0 {
		req.Rows.RowRanges[0].EndKey = nil
	}
	req.Filter = readFilter(family, qualifiers...)
	// Cancel on return so an early callback exit tears down the server stream
	// instead of leaving it to the connection finalizer.
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	stream, err := c.data.ReadRows(streamCtx, req)
	if err != nil {
		return err
	}
	var builder rowBuilder
	for {
		resp, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		for _, chunk := range resp.Chunks {
			row, committed, err := builder.add(chunk)
			if err != nil {
				return err
			}
			if committed {
				rowsRead++
				bytesRead += rowSize(row)
				if !f(row) {
					return nil
				}
			}
		}
	}
}

func (c *Client) ApplyBulk(ctx context.Context, rows []RowMutation) ([]error, error) {
	if len(rows) == 0 {
		return nil, nil
	}
	start := time.Now()
	var bytesWritten int64
	defer func() {
		c.metrics.recordWrite(ctx, c.table, time.Since(start), int64(len(rows)), bytesWritten)
	}()
	entries := make([]*bigtablepb.MutateRowsRequest_Entry, 0, len(rows))
	for _, row := range rows {
		bytesWritten += mutationSize(row)
		entry := &bigtablepb.MutateRowsRequest_Entry{
			RowKey:    []byte(row.RowKey),
			Mutations: make([]*bigtablepb.Mutation, 0, len(row.SetCells)),
		}
		for _, cell := range row.SetCells {
			entry.Mutations = append(entry.Mutations, &bigtablepb.Mutation{
				Mutation: &bigtablepb.Mutation_SetCell_{SetCell: &bigtablepb.Mutation_SetCell{
					FamilyName:      cell.Family,
					ColumnQualifier: []byte(cell.Qualifier),
					TimestampMicros: cell.TimestampMicros,
					Value:           cell.Value,
				}},
			})
		}
		entries = append(entries, entry)
	}
	stream, err := c.data.MutateRows(ctx, &bigtablepb.MutateRowsRequest{
		TableName:    c.tableName,
		AppProfileId: c.appProfile,
		Entries:      entries,
	})
	if err != nil {
		return nil, err
	}
	rowErrs := make([]error, len(rows))
	seen := make([]bool, len(rows))
	for {
		resp, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return rowErrs, err
		}
		for _, entry := range resp.Entries {
			if entry.Index < 0 || int(entry.Index) >= len(rowErrs) {
				return rowErrs, fmt.Errorf("bigtable returned invalid mutation index %d", entry.Index)
			}
			idx := int(entry.Index)
			seen[idx] = true
			if st := entry.Status; st != nil && st.Code != 0 {
				rowErrs[idx] = fmt.Errorf("bigtable status %d: %s", st.Code, st.Message)
			}
		}
	}
	for i := range seen {
		if !seen[i] {
			rowErrs[i] = fmt.Errorf("bigtable missing mutation result")
		}
	}
	return rowErrs, nil
}

// LastVersion scans every version-marker bucket in parallel and returns the
// highest ingested version.
func LastVersion(ctx context.Context, readRows ReadRowsFunc) (int64, error) {
	versions := make([]int64, VersionBucketCount)
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(defaultReadWorkers)
	for bucket := 0; bucket < VersionBucketCount; bucket++ {
		bucket := bucket
		g.Go(func() error {
			prefix := versionRowPrefix(bucket)
			var bucketVersion int64
			err := readRows(gctx, prefix, prefixEnd(prefix), 1, "", func(row Row) bool {
				if version, ok := VersionFromRowKey(row.Key); ok {
					bucketVersion = version
				}
				return false
			})
			if err != nil {
				return fmt.Errorf("read latest bigtable version bucket %d: %w", bucket, err)
			}
			versions[bucket] = bucketVersion
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return 0, err
	}
	var maxVersion int64
	for _, version := range versions {
		if version > maxVersion {
			maxVersion = version
		}
	}
	return maxVersion, nil
}

func mutationRowPrefixBytes(storeName string, key []byte, shards int) []byte {
	shards = normalizeShards(shards)
	shard := shardOf(storeName, key, shards)
	prefixLen := 1 + 2 + 2 + len(storeName) + 4 + len(key)
	prefix := make([]byte, prefixLen, prefixLen+8)
	prefix[0] = mutationPrefix
	binary.BigEndian.PutUint16(prefix[1:], shard)
	binary.BigEndian.PutUint16(prefix[3:], uint16FromBoundedInt(len(storeName)))
	copy(prefix[5:], storeName)
	keyOffset := 5 + len(storeName)
	binary.BigEndian.PutUint32(prefix[keyOffset:], uint32FromBoundedInt(len(key)))
	copy(prefix[keyOffset+4:], key)
	return prefix
}

func MutationRowKey(storeName string, key []byte, version int64, shards int) string {
	return string(mutationRowKeyBytes(storeName, key, version, shards))
}

func mutationRowKeyBytes(storeName string, key []byte, version int64, shards int) []byte {
	prefix := mutationRowPrefixBytes(storeName, key, shards)
	return append(prefix, invertedVersion(version)...)
}

// MutationRowRange returns the start key of a point lookup at
// targetVersion and the exclusive end of the key's version range, hashing and
// encoding the shared row prefix only once.
func MutationRowRange(storeName string, key []byte, targetVersion int64, shards int) (start, end []byte) {
	prefix := mutationRowPrefixBytes(storeName, key, shards)
	end = prefixEnd(prefix)
	start = append(prefix, invertedVersion(targetVersion)...)
	return start, end
}

func VersionRowKey(version int64) string {
	prefix := versionRowPrefix(VersionBucket(version))
	return string(append(prefix, invertedVersion(version)...))
}

func UpgradeRowKey(version int64, name string) string {
	key := make([]byte, 1+8+2+len(name))
	key[0] = upgradePrefix
	copy(key[1:], invertedVersion(version))
	binary.BigEndian.PutUint16(key[9:], uint16FromBoundedInt(len(name)))
	copy(key[11:], name)
	return string(key)
}

// VersionFromRowKey extracts the version encoded in a version-marker or
// mutation row key.
func VersionFromRowKey(rowKey string) (int64, bool) {
	key := []byte(rowKey)
	switch {
	case len(key) >= 1+2+8 && key[0] == versionPrefix:
		return decodeInvertedVersion(key[3:11])
	case len(key) >= 8 && key[0] == mutationPrefix:
		return decodeInvertedVersion(key[len(key)-8:])
	default:
		return 0, false
	}
}

func Timestamp(version int64) int64 {
	return version * int64(time.Millisecond/time.Microsecond)
}

type rowBuilder struct {
	key       []byte
	family    string
	qualifier string
	value     []byte
	inCell    bool
	cells     []Cell
}

func (b *rowBuilder) add(chunk *bigtablepb.ReadRowsResponse_CellChunk) (Row, bool, error) {
	if chunk.GetResetRow() {
		b.reset()
		return Row{}, false, nil
	}
	if len(chunk.RowKey) != 0 {
		b.key = append(b.key[:0], chunk.RowKey...)
		b.family = ""
		b.qualifier = ""
		b.value = nil
		b.inCell = false
		b.cells = b.cells[:0]
	}
	if chunk.FamilyName != nil {
		b.family = chunk.FamilyName.Value
	}
	if chunk.Qualifier != nil {
		b.qualifier = string(chunk.Qualifier.Value)
		b.value = b.value[:0]
		b.inCell = true
	}
	// A non-final chunk of a split cell advertises the total value size; grow
	// the buffer once instead of re-growing on every continuation chunk.
	if size := int(chunk.ValueSize); size > cap(b.value) {
		grown := make([]byte, len(b.value), size)
		copy(grown, b.value)
		b.value = grown
	}
	b.value = append(b.value, chunk.Value...)
	if b.inCell && chunk.ValueSize == 0 {
		b.cells = append(b.cells, Cell{
			Family:    b.family,
			Qualifier: b.qualifier,
			Value:     append([]byte(nil), b.value...),
		})
		b.value = nil
		b.inCell = false
	}
	if !chunk.GetCommitRow() {
		return Row{}, false, nil
	}
	if len(b.key) == 0 {
		return Row{}, false, fmt.Errorf("bigtable committed row without key")
	}
	row := Row{
		Key:   string(append([]byte(nil), b.key...)),
		Cells: append([]Cell(nil), b.cells...),
	}
	b.reset()
	return row, true, nil
}

func (b *rowBuilder) reset() {
	b.key = nil
	b.family = ""
	b.qualifier = ""
	b.value = nil
	b.inCell = false
	b.cells = nil
}

func fullTableName(projectID, instanceID, table string) string {
	return fmt.Sprintf("projects/%s/instances/%s/tables/%s", projectID, instanceID, table)
}

func readFilter(family string, qualifiers ...string) *bigtablepb.RowFilter {
	filters := make([]*bigtablepb.RowFilter, 0, 2)
	if family != "" {
		filters = append(filters, &bigtablepb.RowFilter{
			Filter: &bigtablepb.RowFilter_FamilyNameRegexFilter{FamilyNameRegexFilter: regexp.QuoteMeta(family)},
		})
	}
	if qualifierFilter := qualifiersFilter(qualifiers...); qualifierFilter != nil {
		filters = append(filters, qualifierFilter)
	}
	switch len(filters) {
	case 0:
		return nil
	case 1:
		return filters[0]
	default:
		return &bigtablepb.RowFilter{
			Filter: &bigtablepb.RowFilter_Chain_{Chain: &bigtablepb.RowFilter_Chain{Filters: filters}},
		}
	}
}

func qualifiersFilter(qualifiers ...string) *bigtablepb.RowFilter {
	filters := make([]*bigtablepb.RowFilter, 0, len(qualifiers))
	for _, qualifier := range qualifiers {
		if qualifier == "" {
			continue
		}
		filters = append(filters, &bigtablepb.RowFilter{
			Filter: &bigtablepb.RowFilter_ColumnQualifierRegexFilter{
				ColumnQualifierRegexFilter: []byte(regexp.QuoteMeta(qualifier)),
			},
		})
	}
	switch len(filters) {
	case 0:
		return nil
	case 1:
		return filters[0]
	default:
		return &bigtablepb.RowFilter{
			Filter: &bigtablepb.RowFilter_Interleave_{Interleave: &bigtablepb.RowFilter_Interleave{Filters: filters}},
		}
	}
}

func versionRowPrefix(bucket int) []byte {
	prefix := make([]byte, 1+2)
	prefix[0] = versionPrefix
	binary.BigEndian.PutUint16(prefix[1:], uint16FromBoundedInt(bucket))
	return prefix
}

func prefixEnd(prefix []byte) []byte {
	end := append([]byte(nil), prefix...)
	for i := len(end) - 1; i >= 0; i-- {
		if end[i] != 0xff {
			end[i]++
			return end[:i+1]
		}
	}
	return nil
}

func invertedVersion(version int64) []byte {
	out := make([]byte, 8)
	binary.BigEndian.PutUint64(out, ^uint64FromNonNegativeInt64(version))
	return out
}

func decodeInvertedVersion(encoded []byte) (int64, bool) {
	version := ^binary.BigEndian.Uint64(encoded)
	if version > maxInt64Uint64 {
		return 0, false
	}
	// #nosec G115 -- version is checked above to fit in int64.
	return int64(version), true
}

func shardOf(storeName string, key []byte, shards int) uint16 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(storeName))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write(key)
	return uint16FromBoundedUint32(h.Sum32() % uint32FromBoundedInt(shards))
}

func normalizeShards(shards int) int {
	if shards <= 0 {
		return DefaultShards
	}
	if shards > maxUint16Int {
		return maxUint16Int
	}
	return shards
}

func uint16FromBoundedInt(value int) uint16 {
	if value < 0 || value > maxUint16Int {
		panic(fmt.Sprintf("bigtable value %d exceeds uint16", value))
	}
	// #nosec G115 -- value is checked above to fit in uint16.
	return uint16(value)
}

func uint32FromBoundedInt(value int) uint32 {
	if value < 0 || value > maxUint32Int {
		panic(fmt.Sprintf("bigtable value %d exceeds uint32", value))
	}
	// #nosec G115 -- value is checked above to fit in uint32.
	return uint32(value)
}

func uint16FromBoundedUint32(value uint32) uint16 {
	if value > maxUint16Int {
		panic(fmt.Sprintf("bigtable value %d exceeds uint16", value))
	}
	// #nosec G115 -- value is checked above to fit in uint16.
	return uint16(value)
}

func uint64FromNonNegativeInt64(value int64) uint64 {
	if value < 0 {
		panic(fmt.Sprintf("bigtable version %d is negative", value))
	}
	// #nosec G115 -- value is checked above to be non-negative.
	return uint64(value)
}
