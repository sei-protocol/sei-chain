package historical

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
	DefaultBigtableFamily = "state"
	DefaultBigtableShards = 256

	bigtableMutationPrefix = byte('m')
	bigtableVersionPrefix  = byte('v')
	bigtableUpgradePrefix  = byte('u')

	BigtableValueColumn   = "value"
	BigtableDeletedColumn = "deleted"
)

const (
	bigtableEndpoint = "bigtable.googleapis.com:443"

	defaultBigtableReadWorkers = 16

	// Transient stream failures (tablet moves, GFE restarts) surface as
	// UNAVAILABLE; retry reads a bounded number of times with backoff.
	bigtableReadAttempts     = 3
	bigtableReadRetryBackoff = 100 * time.Millisecond

	maxUint16Int   = 1<<16 - 1
	maxUint32Int   = 1<<32 - 1
	maxInt64Uint64 = 1<<63 - 1
)

// BigtableClient speaks the Bigtable data protocol directly over gRPC. The
// official cloud.google.com/go/bigtable client requires a newer
// google.golang.org/grpc than the repo-wide v1.57 replace pin allows, so this
// package carries its own thin transport: ReadRows chunk assembly, MutateRows
// result accounting, bounded read retries, and per-RPC cost metrics.
type BigtableClient struct {
	conn       *grpc.ClientConn
	data       bigtablepb.BigtableClient
	tableName  string
	appProfile string
	// table is the short table name used as the metric attribute.
	table   string
	metrics *bigtableMetrics
}

type BigtableCell struct {
	Family    string
	Qualifier string
	Value     []byte
}

type BigtableRow struct {
	Key   string
	Cells []BigtableCell
}

type BigtableSetCell struct {
	Family          string
	Qualifier       string
	TimestampMicros int64
	Value           []byte
}

type BigtableRowMutation struct {
	RowKey   string
	SetCells []BigtableSetCell
}

type BigtableConfig struct {
	ProjectID  string
	InstanceID string
	Table      string
	Family     string
	AppProfile string
	Shards     int
}

func (c *BigtableConfig) ApplyDefaults() {
	if c.Family == "" {
		c.Family = DefaultBigtableFamily
	}
	if c.Shards == 0 {
		c.Shards = DefaultBigtableShards
	}
}

func (c BigtableConfig) Configured() bool {
	return strings.TrimSpace(c.ProjectID) != "" ||
		strings.TrimSpace(c.InstanceID) != "" ||
		strings.TrimSpace(c.Table) != ""
}

func (c *BigtableConfig) Validate() error {
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

func NewBigtableReader(cfg BigtableConfig) (Reader, error) {
	cfg.ApplyDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	client, err := OpenBigtableClient(context.Background(), cfg)
	if err != nil {
		return nil, err
	}
	return &bigtableReader{
		client:   client,
		readRows: client.ReadRows,
		family:   cfg.Family,
		shards:   cfg.Shards,
	}, nil
}

func OpenBigtableClient(ctx context.Context, cfg BigtableConfig) (*BigtableClient, error) {
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
	return &BigtableClient{
		conn:       conn,
		data:       bigtablepb.NewBigtableClient(conn),
		tableName:  bigtableTableName(cfg.ProjectID, cfg.InstanceID, cfg.Table),
		appProfile: cfg.AppProfile,
		table:      cfg.Table,
		metrics:    newBigtableMetrics(),
	}, nil
}

type BigtableReadRowsFunc func(ctx context.Context, startKey, endKey []byte, limit int64, family string, f func(BigtableRow) bool, qualifiers ...string) error

type BigtableApplyBulkFunc func(ctx context.Context, rows []BigtableRowMutation) ([]error, error)

func (c *BigtableClient) Close() error {
	if c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

// ReadRows retries transient stream failures as long as no row has been
// delivered to the callback yet, which covers every limit-1 point read and
// version-bucket scan in this package.
func (c *BigtableClient) ReadRows(ctx context.Context, startKey, endKey []byte, limit int64, family string, f func(BigtableRow) bool, qualifiers ...string) error {
	return readRowsWithRetry(ctx, c.readRowsOnce, startKey, endKey, limit, family, f, qualifiers...)
}

func readRowsWithRetry(ctx context.Context, once BigtableReadRowsFunc, startKey, endKey []byte, limit int64, family string, f func(BigtableRow) bool, qualifiers ...string) error {
	backoff := bigtableReadRetryBackoff
	var delivered bool
	for attempt := 1; ; attempt++ {
		err := once(ctx, startKey, endKey, limit, family, func(row BigtableRow) bool {
			delivered = true
			return f(row)
		}, qualifiers...)
		if err == nil || delivered || attempt >= bigtableReadAttempts || status.Code(err) != codes.Unavailable {
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

func (c *BigtableClient) readRowsOnce(ctx context.Context, startKey, endKey []byte, limit int64, family string, f func(BigtableRow) bool, qualifiers ...string) error {
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
	req.Filter = bigtableReadFilter(family, qualifiers...)
	// Cancel on return so an early callback exit tears down the server stream
	// instead of leaving it to the connection finalizer.
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	stream, err := c.data.ReadRows(streamCtx, req)
	if err != nil {
		return err
	}
	var builder bigtableRowBuilder
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
				bytesRead += bigtableRowSize(row)
				if !f(row) {
					return nil
				}
			}
		}
	}
}

func (c *BigtableClient) ApplyBulk(ctx context.Context, rows []BigtableRowMutation) ([]error, error) {
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
		bytesWritten += bigtableMutationSize(row)
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

type bigtableReader struct {
	client   *BigtableClient
	readRows BigtableReadRowsFunc
	family   string
	shards   int
}

var _ Reader = (*bigtableReader)(nil)

func (r *bigtableReader) Close() error {
	if r.client != nil {
		return r.client.Close()
	}
	return nil
}

func (r *bigtableReader) LastVersion(ctx context.Context) (int64, error) {
	return BigtableLastVersion(ctx, r.readRows)
}

func (r *bigtableReader) Has(ctx context.Context, storeName string, key []byte, targetVersion int64) (bool, error) {
	start, end := bigtableMutationRowRange(storeName, key, targetVersion, r.shards)
	var row BigtableRow
	err := r.readRows(ctx, start, end, 1, r.family, func(r BigtableRow) bool {
		row = r
		return false
	}, BigtableDeletedColumn)
	if err != nil {
		return false, fmt.Errorf("bigtable has lookup: %w", err)
	}
	if row.Key == "" {
		return false, nil
	}
	deleted, err := bigtableDeletedFromRow(row, r.family)
	if err != nil {
		return false, err
	}
	return !deleted, nil
}

func (r *bigtableReader) Get(ctx context.Context, storeName string, key []byte, targetVersion int64) (Value, error) {
	start, end := bigtableMutationRowRange(storeName, key, targetVersion, r.shards)
	var row BigtableRow
	err := r.readRows(ctx, start, end, 1, r.family, func(r BigtableRow) bool {
		row = r
		return false
	}, BigtableValueColumn, BigtableDeletedColumn)
	if err != nil {
		return Value{}, fmt.Errorf("bigtable get lookup: %w", err)
	}
	if row.Key == "" {
		return Value{}, ErrNotFound
	}
	return bigtableValueFromRow(row, r.family)
}

func BigtableLastVersion(ctx context.Context, readRows BigtableReadRowsFunc) (int64, error) {
	versions := make([]int64, VersionBucketCount)
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(defaultBigtableReadWorkers)
	for bucket := 0; bucket < VersionBucketCount; bucket++ {
		bucket := bucket
		g.Go(func() error {
			prefix := bigtableVersionRowPrefix(bucket)
			var bucketVersion int64
			err := readRows(gctx, prefix, bigtablePrefixEnd(prefix), 1, "", func(row BigtableRow) bool {
				if version, ok := bigtableVersionFromRowKey(row.Key); ok {
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

// bigtableValueFromRow interprets a mutation row. The returned Value aliases
// the row's cell buffer, which the row builder allocates per cell.
func bigtableValueFromRow(row BigtableRow, family string) (Value, error) {
	version, ok := bigtableVersionFromRowKey(row.Key)
	if !ok {
		return Value{}, fmt.Errorf("invalid bigtable mutation row key")
	}
	var value []byte
	deleted := false
	for _, cell := range row.Cells {
		if cell.Family != family {
			continue
		}
		switch cell.Qualifier {
		case BigtableValueColumn:
			value = cell.Value
		case BigtableDeletedColumn:
			deleted = len(cell.Value) > 0 && cell.Value[0] == 1
		}
	}
	if deleted || value == nil {
		return Value{}, ErrNotFound
	}
	return Value{Bytes: value, Version: version}, nil
}

func bigtableDeletedFromRow(row BigtableRow, family string) (bool, error) {
	if _, ok := bigtableVersionFromRowKey(row.Key); !ok {
		return false, fmt.Errorf("invalid bigtable mutation row key")
	}
	for _, cell := range row.Cells {
		if cell.Family == family && cell.Qualifier == BigtableDeletedColumn {
			return len(cell.Value) > 0 && cell.Value[0] == 1, nil
		}
	}
	return false, nil
}

func bigtableMutationRowPrefixBytes(storeName string, key []byte, shards int) []byte {
	shards = normalizeBigtableShards(shards)
	shard := bigtableShard(storeName, key, shards)
	prefixLen := 1 + 2 + 2 + len(storeName) + 4 + len(key)
	prefix := make([]byte, prefixLen, prefixLen+8)
	prefix[0] = bigtableMutationPrefix
	binary.BigEndian.PutUint16(prefix[1:], shard)
	binary.BigEndian.PutUint16(prefix[3:], uint16FromBoundedInt(len(storeName)))
	copy(prefix[5:], storeName)
	keyOffset := 5 + len(storeName)
	binary.BigEndian.PutUint32(prefix[keyOffset:], uint32FromBoundedInt(len(key)))
	copy(prefix[keyOffset+4:], key)
	return prefix
}

func BigtableMutationRowKey(storeName string, key []byte, version int64, shards int) string {
	return string(bigtableMutationRowKeyBytes(storeName, key, version, shards))
}

func bigtableMutationRowKeyBytes(storeName string, key []byte, version int64, shards int) []byte {
	prefix := bigtableMutationRowPrefixBytes(storeName, key, shards)
	return append(prefix, bigtableInvertedVersion(version)...)
}

// bigtableMutationRowRange returns the start key of a point lookup at
// targetVersion and the exclusive end of the key's version range, hashing and
// encoding the shared row prefix only once.
func bigtableMutationRowRange(storeName string, key []byte, targetVersion int64, shards int) (start, end []byte) {
	prefix := bigtableMutationRowPrefixBytes(storeName, key, shards)
	end = bigtablePrefixEnd(prefix)
	start = append(prefix, bigtableInvertedVersion(targetVersion)...)
	return start, end
}

func BigtableVersionRowKey(version int64) string {
	prefix := bigtableVersionRowPrefix(VersionBucket(version))
	return string(append(prefix, bigtableInvertedVersion(version)...))
}

func BigtableUpgradeRowKey(version int64, name string) string {
	key := make([]byte, 1+8+2+len(name))
	key[0] = bigtableUpgradePrefix
	copy(key[1:], bigtableInvertedVersion(version))
	binary.BigEndian.PutUint16(key[9:], uint16FromBoundedInt(len(name)))
	copy(key[11:], name)
	return string(key)
}

func bigtableVersionFromRowKey(rowKey string) (int64, bool) {
	key := []byte(rowKey)
	switch {
	case len(key) >= 1+2+8 && key[0] == bigtableVersionPrefix:
		return bigtableDecodeInvertedVersion(key[3:11])
	case len(key) >= 8 && key[0] == bigtableMutationPrefix:
		return bigtableDecodeInvertedVersion(key[len(key)-8:])
	default:
		return 0, false
	}
}

func BigtableTimestamp(version int64) int64 {
	return version * int64(time.Millisecond/time.Microsecond)
}

type bigtableRowBuilder struct {
	key       []byte
	family    string
	qualifier string
	value     []byte
	inCell    bool
	cells     []BigtableCell
}

func (b *bigtableRowBuilder) add(chunk *bigtablepb.ReadRowsResponse_CellChunk) (BigtableRow, bool, error) {
	if chunk.GetResetRow() {
		b.reset()
		return BigtableRow{}, false, nil
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
		b.cells = append(b.cells, BigtableCell{
			Family:    b.family,
			Qualifier: b.qualifier,
			Value:     append([]byte(nil), b.value...),
		})
		b.value = nil
		b.inCell = false
	}
	if !chunk.GetCommitRow() {
		return BigtableRow{}, false, nil
	}
	if len(b.key) == 0 {
		return BigtableRow{}, false, fmt.Errorf("bigtable committed row without key")
	}
	row := BigtableRow{
		Key:   string(append([]byte(nil), b.key...)),
		Cells: append([]BigtableCell(nil), b.cells...),
	}
	b.reset()
	return row, true, nil
}

func (b *bigtableRowBuilder) reset() {
	b.key = nil
	b.family = ""
	b.qualifier = ""
	b.value = nil
	b.inCell = false
	b.cells = nil
}

func bigtableTableName(projectID, instanceID, table string) string {
	return fmt.Sprintf("projects/%s/instances/%s/tables/%s", projectID, instanceID, table)
}

func bigtableReadFilter(family string, qualifiers ...string) *bigtablepb.RowFilter {
	filters := make([]*bigtablepb.RowFilter, 0, 2)
	if family != "" {
		filters = append(filters, &bigtablepb.RowFilter{
			Filter: &bigtablepb.RowFilter_FamilyNameRegexFilter{FamilyNameRegexFilter: regexp.QuoteMeta(family)},
		})
	}
	if qualifierFilter := bigtableQualifierFilter(qualifiers...); qualifierFilter != nil {
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

func bigtableQualifierFilter(qualifiers ...string) *bigtablepb.RowFilter {
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

func bigtableVersionRowPrefix(bucket int) []byte {
	prefix := make([]byte, 1+2)
	prefix[0] = bigtableVersionPrefix
	binary.BigEndian.PutUint16(prefix[1:], uint16FromBoundedInt(bucket))
	return prefix
}

func bigtablePrefixEnd(prefix []byte) []byte {
	end := append([]byte(nil), prefix...)
	for i := len(end) - 1; i >= 0; i-- {
		if end[i] != 0xff {
			end[i]++
			return end[:i+1]
		}
	}
	return nil
}

func bigtableInvertedVersion(version int64) []byte {
	out := make([]byte, 8)
	binary.BigEndian.PutUint64(out, ^uint64FromNonNegativeInt64(version))
	return out
}

func bigtableDecodeInvertedVersion(encoded []byte) (int64, bool) {
	version := ^binary.BigEndian.Uint64(encoded)
	if version > maxInt64Uint64 {
		return 0, false
	}
	// #nosec G115 -- version is checked above to fit in int64.
	return int64(version), true
}

func bigtableShard(storeName string, key []byte, shards int) uint16 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(storeName))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write(key)
	return uint16FromBoundedUint32(h.Sum32() % uint32FromBoundedInt(shards))
}

func normalizeBigtableShards(shards int) int {
	if shards <= 0 {
		return DefaultBigtableShards
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
