package operations

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

// goldenInspectPlanItem returns the plan item of one golden inspection, with the list limit the
// single run reads from its flags.
func goldenInspectPlanItem(t *testing.T, inspect map[string]string, out string) inspectPlanItem {
	t.Helper()
	atoi := func(name, fallback string) int {
		value, ok := inspect[name]
		if !ok {
			value = fallback
		}
		n, err := strconv.Atoi(value)
		require.NoError(t, err, "flag %s", name)
		return n
	}
	return inspectPlanItem{
		InspectBucket:  inspect["inspect-bucket"],
		KeyOffset:      atoi("key-offset", "0"),
		KeyPrefix:      inspect["key-prefix"],
		ShardNextBytes: atoi("shard-next-bytes", "0"),
		List:           inspect["list"] == "true",
		ListLimit:      atoi("list-limit", strconv.Itoa(defaultInspectListLimit)),
		Out:            out,
	}
}

func writeInspectPlan(t *testing.T, items any) string {
	t.Helper()
	data, err := json.Marshal(items)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "plan.json")
	require.NoError(t, os.WriteFile(path, data, 0o600))
	return path
}

func runInspectPlan(t *testing.T, source map[string]string, planPath string) error {
	t.Helper()
	cmd := newEvmDigestGoldenCmd(t, source, map[string]string{"inspect-plan": planPath})
	captureDigestOutput(t, true)
	return runEvmLogicalDigest(cmd, nil)
}

// TestEvmLogicalInspectPlanMatchesSingleRuns requires each plan output to be byte-equal to the
// report of the same inspection run on its own, for every source that supports inspect.
func TestEvmLogicalInspectPlanMatchesSingleRuns(t *testing.T) {
	fx := buildEvmDigestGoldenFixture(t)
	inspections := evmDigestGoldenInspections()
	names := make([]string, 0, len(inspections))
	for name := range inspections {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, src := range evmDigestGoldenSources(fx) {
		if !src.inspect {
			continue
		}
		t.Run(src.name, func(t *testing.T) {
			outDir := t.TempDir()
			items := make([]inspectPlanItem, 0, len(names))
			for _, name := range names {
				items = append(items, goldenInspectPlanItem(t, inspections[name], filepath.Join(outDir, name+".json")))
			}
			require.NoError(t, runInspectPlan(t, src.flags, writeInspectPlan(t, items)))

			for i, name := range names {
				cmd := newEvmDigestGoldenCmd(t, src.flags, inspections[name])
				_, single := captureDigestOutput(t, true)
				require.NoError(t, runEvmLogicalDigest(cmd, nil))
				planned, err := os.ReadFile(items[i].Out)
				require.NoError(t, err)
				require.Equal(t, single.String(), string(planned), "inspection %s", name)
			}
		})
	}
}

func TestEvmLogicalInspectPlanOmittedListLimitListsEveryMatch(t *testing.T) {
	fx := buildEvmDigestGoldenFixture(t)
	out := filepath.Join(t.TempDir(), "storage.json")
	plan := writeInspectPlan(t, []map[string]any{{
		"inspect_bucket": flatkvBucketStorage,
		"key_offset":     len("evm/"),
		"list":           true,
		"out":            out,
	}})
	require.NoError(t, runInspectPlan(t, goldenFlatKVSource(fx, goldenTipHeight), plan))

	data, err := os.ReadFile(out)
	require.NoError(t, err)
	var report evmInspectJSON
	require.NoError(t, json.Unmarshal(data, &report))
	require.Zero(t, report.ListLimit)
	require.Greater(t, report.Matched, uint64(2))
	require.Equal(t, int(report.Matched), report.Listed)
}

func TestEvmLogicalInspectPlanRefusesInvalidPlans(t *testing.T) {
	fx := buildEvmDigestGoldenFixture(t)
	source := goldenFlatKVSource(fx, goldenTipHeight)
	out := filepath.Join(t.TempDir(), "out.json")
	item := func(edit func(map[string]any)) map[string]any {
		m := map[string]any{"inspect_bucket": flatkvBucketStorage, "list": true, "out": out}
		edit(m)
		return m
	}
	for name, tc := range map[string]struct {
		plan any
		want string
	}{
		"no items":       {[]any{}, "has no items"},
		"unknown bucket": {[]any{item(func(m map[string]any) { m["inspect_bucket"] = "nonce" })}, `unknown inspect_bucket "nonce"`},
		"details":        {[]any{item(func(m map[string]any) { m["details"] = true })}, "details is not supported in a plan"},
		"empty out":      {[]any{item(func(m map[string]any) { delete(m, "out") })}, "out is empty"},
		"duplicate out":  {[]any{item(func(map[string]any) {}), item(func(map[string]any) {})}, "items 0 and 1 both write"},
		"unknown field":  {[]any{item(func(m map[string]any) { m["listlimit"] = 1 })}, `unknown field "listlimit"`},
		"bad prefix":     {[]any{item(func(m map[string]any) { m["key_prefix"] = "0x03" })}, "decode key_prefix"},
	} {
		t.Run(name, func(t *testing.T) {
			require.ErrorContains(t, runInspectPlan(t, source, writeInspectPlan(t, tc.plan)), tc.want)
		})
	}

	t.Run("single inspect flag", func(t *testing.T) {
		plan := writeInspectPlan(t, []any{item(func(map[string]any) {})})
		cmd := newEvmDigestGoldenCmd(t, source, map[string]string{"inspect-plan": plan, "list-limit": "0"})
		captureDigestOutput(t, true)
		require.ErrorContains(t, runEvmLogicalDigest(cmd, nil), "--inspect-plan replaces --list-limit")
	})
}
