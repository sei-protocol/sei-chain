package kvrepair

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

func knownStore(name string) bool {
	return name == "evm" || name == "bank"
}

func parseAndValidate(data string) (Repair, error) {
	r, err := Parse([]byte(data))
	if err != nil {
		return Repair{}, err
	}
	return r, r.Validate(knownStore)
}

func hexPtr(b []byte) *HexBytes {
	h := HexBytes(b)
	return &h
}

func TestParseRepair(t *testing.T) {
	r, err := parseAndValidate(`{"name":"a","chain_id":"c","target_repair_height":10,"state_height":9,"source":"reserve at 9","entries":[
		{"store":"evm","key":"0x0102","new":"aa","old":"bb"},
		{"store":"evm","key":"0304","new":null,"old_absent":false}]}`)
	require.NoError(t, err)
	require.Equal(t, "a", r.Name)
	require.Equal(t, int64(10), r.TargetRepairHeight)
	require.Equal(t, "reserve at 9", r.Source)
	require.Equal(t, HexBytes{1, 2}, r.Entries[0].Key)
	require.Equal(t, hexPtr([]byte{0xaa}), r.Entries[0].New)
	require.Equal(t, hexPtr([]byte{0xbb}), r.Entries[0].Old)
	require.Nil(t, r.Entries[1].New)
}

func TestParseAndValidateRejectInvalidRepairs(t *testing.T) {
	for name, tc := range map[string]struct {
		file string
		err  string
	}{
		"unknown field": {
			file: `{"name":"a","chain_id":"c","target_repair_height":2,"state_height":1,"entries":[{"store":"evm","key":"01","ne":"02"}]}`,
			err:  "unknown field",
		},
		"bad hex": {
			file: `{"name":"a","chain_id":"c","target_repair_height":2,"state_height":1,"entries":[{"store":"evm","key":"zz","new":"02"}]}`,
			err:  "invalid hex",
		},
		"unknown store": {
			file: `{"name":"a","chain_id":"c","target_repair_height":2,"state_height":1,"entries":[{"store":"nope","key":"01","new":"02"}]}`,
			err:  `unknown store "nope"`,
		},
		"no entries": {
			file: `{"name":"a","chain_id":"c","target_repair_height":2,"state_height":1,"entries":[]}`,
			err:  "no entries",
		},
		"zero target repair height": {
			file: `{"name":"a","chain_id":"c","target_repair_height":0,"state_height":1,"entries":[{"store":"evm","key":"01","new":"02"}]}`,
			err:  "not positive",
		},
		"field twice in an entry": {
			file: `{"name":"a","chain_id":"c","target_repair_height":2,"state_height":1,"entries":[{"store":"evm","key":"01","new":"02","new":"03"}]}`,
			err:  `field "new" appears twice`,
		},
		"field twice at the top": {
			file: `{"name":"a","chain_id":"c","target_repair_height":2,"target_repair_height":3,"state_height":1,"entries":[{"store":"evm","key":"01","new":"02"}]}`,
			err:  `field "target_repair_height" appears twice`,
		},
		"field name in another case": {
			file: `{"name":"a","chain_id":"c","target_repair_height":2,"state_height":1,"entries":[{"store":"evm","key":"01","new":"02","NEW":"03"}]}`,
			err:  `field name "NEW" must use lowercase`,
		},
		"field name with a folding rune": {
			file: `{"name":"a","chain_id":"c","target_repair_height":2,"state_height":1,"entries":[{"store":"evm","\u212aey":"01","new":"02"}]}`,
			err:  "must use lowercase",
		},
		"both old values": {
			file: `{"name":"a","chain_id":"c","target_repair_height":2,"state_height":1,"entries":[{"store":"evm","key":"01","new":"02","old":"03","old_absent":true}]}`,
			err:  "both set",
		},
		"second object": {
			file: `{"name":"a","chain_id":"c","target_repair_height":2,"state_height":1,"entries":[{"store":"evm","key":"01","new":"02"}]}
{"name":"b","chain_id":"c","target_repair_height":3,"state_height":2,"entries":[{"store":"evm","key":"02","new":"02"}]}`,
			err: "unexpected data after the JSON value",
		},
		"trailing garbage": {
			file: `{"name":"a","chain_id":"c","target_repair_height":2,"state_height":1,"entries":[{"store":"evm","key":"01","new":"02"}]} xyz`,
			err:  "unexpected data after the JSON value",
		},
		"missing new": {
			file: `{"name":"a","chain_id":"c","target_repair_height":2,"state_height":1,"entries":[{"store":"evm","key":"01","old":"03"}]}`,
			err:  "new is missing",
		},
		"null old": {
			file: `{"name":"a","chain_id":"c","target_repair_height":2,"state_height":1,"entries":[{"store":"evm","key":"01","new":"02","old":null}]}`,
			err:  "old is null",
		},
		"no state height": {
			file: `{"name":"a","chain_id":"c","target_repair_height":2,"entries":[{"store":"evm","key":"01","new":"02"}]}`,
			err:  "state_height 0 is not positive",
		},
		"state height at target repair height": {
			file: `{"name":"a","chain_id":"c","target_repair_height":2,"state_height":2,"entries":[{"store":"evm","key":"01","new":"02"}]}`,
			err:  "not below target_repair_height",
		},
		"no old value with a gap": {
			file: `{"name":"a","chain_id":"c","target_repair_height":5,"state_height":3,"entries":[{"store":"evm","key":"01","new":"02","old":"03"},{"store":"evm","key":"02","new":"02"}]}`,
			err:  "entry 1: no old value",
		},
		"duplicate key in one file": {
			file: `{"name":"a","chain_id":"c","target_repair_height":2,"state_height":1,"entries":[{"store":"evm","key":"01","new":"02"},{"store":"evm","key":"01","new":"03"}]}`,
			err:  "appears twice",
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := parseAndValidate(tc.file)
			require.ErrorContains(t, err, tc.err)
		})
	}
}

func TestValidateAcceptsGapWhenEveryEntryHasAnOldValue(t *testing.T) {
	r, err := parseAndValidate(`{"name":"a","chain_id":"c","target_repair_height":100,"state_height":10,"entries":[
		{"store":"evm","key":"01","new":"02","old":"03"},
		{"store":"evm","key":"02","new":null,"old_absent":true}]}` + "\n\n")
	require.NoError(t, err)
	require.Equal(t, int64(10), r.StateHeight)
}

func TestParseKeepsEmptyValues(t *testing.T) {
	r, err := parseAndValidate(`{"name":"a","chain_id":"c","target_repair_height":2,"state_height":1,"entries":[
		{"store":"evm","key":"01","new":"","old":"0x"},
		{"store":"evm","key":"02","new":null,"old":""}]}`)
	require.NoError(t, err)
	require.Equal(t, hexPtr([]byte{}), r.Entries[0].New)
	require.NotNil(t, *r.Entries[0].New)
	require.Equal(t, hexPtr([]byte{}), r.Entries[0].Old)
	require.Nil(t, r.Entries[1].New)
	require.Equal(t, hexPtr([]byte{}), r.Entries[1].Old)
}

func TestEncodeWritesOneEntryPerLineAndParsesBack(t *testing.T) {
	r := Repair{Name: "a", ChainID: "c", TargetRepairHeight: 10, StateHeight: 9, Source: "reserve at 9", Entries: []Entry{
		{Store: "evm", Key: HexBytes{0x03, 0x01}, New: hexPtr([]byte{0xaa}), Old: hexPtr([]byte{0xbb})},
		{Store: "evm", Key: HexBytes{0x03, 0x02}, OldAbsent: true},
		{Store: "evm", Key: HexBytes{0x03, 0x03}, New: hexPtr([]byte{}), Old: hexPtr([]byte{0x01})},
	}}
	var b bytes.Buffer
	require.NoError(t, Encode(&b, r))
	require.Equal(t, `{
  "name": "a",
  "chain_id": "c",
  "target_repair_height": 10,
  "state_height": 9,
  "source": "reserve at 9",
  "entries": [
    {"store":"evm","key":"0301","new":"aa","old":"bb"},
    {"store":"evm","key":"0302","new":null,"old_absent":true},
    {"store":"evm","key":"0303","new":"","old":"01"}
  ]
}
`, b.String())

	parsed, err := Parse(b.Bytes())
	require.NoError(t, err)
	require.Equal(t, r, parsed)
}

func TestEncodeOmitsAnEmptySource(t *testing.T) {
	var b bytes.Buffer
	require.NoError(t, Encode(&b, Repair{Name: "a", ChainID: "c", TargetRepairHeight: 2, StateHeight: 1, Entries: []Entry{
		{Store: "evm", Key: HexBytes{1}, New: hexPtr([]byte{2})},
	}}))
	require.NotContains(t, b.String(), "source")
	_, err := parseAndValidate(b.String())
	require.NoError(t, err)
}
