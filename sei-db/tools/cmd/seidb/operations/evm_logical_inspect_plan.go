package operations

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
)

// inspectTarget is one inspect report of a scan, written to out, or through digestOut when out
// is empty.
type inspectTarget struct {
	acc *inspectAccumulator
	out string
}

// inspectFanout feeds every row of one scan to each of its inspect accumulators.
type inspectFanout struct {
	targets []inspectTarget
}

// inspectPlanItem is one entry of an --inspect-plan file.
type inspectPlanItem struct {
	InspectBucket  string `json:"inspect_bucket"`
	KeyOffset      int    `json:"key_offset"`
	KeyPrefix      string `json:"key_prefix"`
	ShardNextBytes int    `json:"shard_next_bytes"`
	List           bool   `json:"list"`
	ListLimit      int    `json:"list_limit"`
	Details        bool   `json:"details"`
	Out            string `json:"out"`
}

// singleInspectFanout returns a fanout that reports acc through digestOut.
func singleInspectFanout(acc *inspectAccumulator) *inspectFanout {
	return &inspectFanout{targets: []inspectTarget{{acc: acc}}}
}

// loadInspectPlan returns a fanout with one accumulator for each item of the plan file at path.
func loadInspectPlan(path string) (*inspectFanout, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("read --inspect-plan: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var items []inspectPlanItem
	if err := dec.Decode(&items); err != nil {
		return nil, fmt.Errorf("decode --inspect-plan %s: %w", path, err)
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("--inspect-plan %s has no items", path)
	}
	f := &inspectFanout{}
	outs := make(map[string]int, len(items))
	for i, item := range items {
		acc, err := item.accumulator()
		if err != nil {
			return nil, fmt.Errorf("--inspect-plan item %d: %w", i, err)
		}
		out := filepath.Clean(item.Out)
		if prev, ok := outs[out]; ok {
			return nil, fmt.Errorf("--inspect-plan items %d and %d both write %s", prev, i, out)
		}
		outs[out] = i
		f.targets = append(f.targets, inspectTarget{acc: acc, out: out})
	}
	return f, nil
}

// accumulator returns the inspect accumulator the item describes.
func (item inspectPlanItem) accumulator() (*inspectAccumulator, error) {
	if !isFlatKVBucket(item.InspectBucket) {
		return nil, fmt.Errorf("unknown inspect_bucket %q", item.InspectBucket)
	}
	if item.Details {
		return nil, errors.New("details is not supported in a plan")
	}
	if item.Out == "" {
		return nil, errors.New("out is empty")
	}
	if item.KeyOffset < 0 {
		return nil, errors.New("key_offset must be non-negative")
	}
	if item.ShardNextBytes < 0 {
		return nil, errors.New("shard_next_bytes must be non-negative")
	}
	keyPrefix, err := hex.DecodeString(item.KeyPrefix)
	if err != nil {
		return nil, fmt.Errorf("decode key_prefix: %w", err)
	}
	return &inspectAccumulator{
		inspectBucket:  item.InspectBucket,
		keyOffset:      item.KeyOffset,
		keyPrefix:      keyPrefix,
		shardNextBytes: item.ShardNextBytes,
		list:           item.List,
		listLimit:      item.ListLimit,
		shards:         make(map[string]*digestBucket),
	}, nil
}

func (f *inspectFanout) consume(physKey, val []byte) error {
	return f.consumeWithMeta(physKey, val, "")
}

// consumeWithMeta normalizes one FlatKV row and feeds it to every accumulator. Only an
// accumulator that lists details receives meta.
func (f *inspectFanout) consumeWithMeta(physKey, val []byte, meta string) error {
	bucket, logical, err := normalizeEVMFlatKVPair(physKey, val)
	if err != nil {
		return err
	}
	for _, t := range f.targets {
		if t.acc.details && t.acc.list {
			t.acc.consumeLogical(bucket, physKey, logical, meta)
		} else {
			t.acc.consumeLogical(bucket, physKey, logical, "")
		}
	}
	return nil
}

func (f *inspectFanout) addLogical(bucket string, physKey, logical, _ []byte) {
	for _, t := range f.targets {
		t.acc.consumeLogical(bucket, physKey, logical, "")
	}
}

// listsDetails reports whether any accumulator lists rows with details.
func (f *inspectFanout) listsDetails() bool {
	for _, t := range f.targets {
		if t.acc.details && t.acc.list {
			return true
		}
	}
	return false
}

// storageDetailsList returns the accumulator of a fanout whose one report is a storage list
// with details, or nil.
func (f *inspectFanout) storageDetailsList() *inspectAccumulator {
	if len(f.targets) != 1 {
		return nil
	}
	acc := f.targets[0].acc
	if acc.details && acc.list && acc.inspectBucket == flatkvBucketStorage {
		return acc
	}
	return nil
}

// inspectsAccounts reports whether any accumulator inspects the account bucket.
func (f *inspectFanout) inspectsAccounts() bool {
	for _, t := range f.targets {
		if t.acc.inspectBucket == flatkvBucketAccount {
			return true
		}
	}
	return false
}

// matchesAccountPhysicalKey reports whether any accumulator inspects the account physKey.
func (f *inspectFanout) matchesAccountPhysicalKey(physKey []byte) bool {
	for _, t := range f.targets {
		if t.acc.matchesAccountPhysicalKey(physKey) {
			return true
		}
	}
	return false
}

// finalizeAccounts feeds the buffered accounts that any accumulator inspects to every
// accumulator, in ascending address order.
func (f *inspectFanout) finalizeAccounts(accounts map[string]*semanticAccountDigestState) {
	addrs := make([]string, 0, len(accounts))
	for addr := range accounts {
		if f.matchesAccountPhysicalKey(ktype.EVMPhysicalKey(keys.EVMKeyNonce, []byte(addr))) {
			addrs = append(addrs, addr)
		}
	}
	sort.Strings(addrs)
	for _, addr := range addrs {
		finalizeSemanticAccount(addr, accounts[addr], f.addLogical, nil)
	}
}

// matched returns the rows matched so far, summed over the accumulators.
func (f *inspectFanout) matched() uint64 {
	var n uint64
	for _, t := range f.targets {
		n += t.acc.matched
	}
	return n
}

// emit writes the report of every accumulator.
func (f *inspectFanout) emit(ctx digestPrintContext) error {
	for _, t := range f.targets {
		if t.out == "" {
			if err := t.acc.emit(ctx); err != nil {
				return err
			}
			continue
		}
		if err := writeInspectPlanReport(t, ctx); err != nil {
			return err
		}
		digestOut.sayf("inspect plan: wrote %s bucket=%s key_prefix=%X matched=%d\n",
			t.out, t.acc.inspectBucket, t.acc.keyPrefix, t.acc.matched)
	}
	return nil
}

func writeInspectPlanReport(t inspectTarget, ctx digestPrintContext) (err error) {
	file, err := os.OpenFile(t.out, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("create inspect plan output: %w", err)
	}
	defer func() {
		if cerr := file.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("close inspect plan output %s: %w", t.out, cerr)
		}
	}()
	if err := t.acc.writeJSON(file, ctx); err != nil {
		return fmt.Errorf("write inspect plan output %s: %w", t.out, err)
	}
	return nil
}
