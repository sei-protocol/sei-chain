package statesync

import (
	"encoding/json"
	"testing"

	"github.com/cockroachdb/pebble/v2"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb"
)

func validManifest() *Manifest {
	return &Manifest{
		FormatVersion:            FormatVersion,
		ChainID:                  testChainID,
		Height:                   7,
		RowCount:                 3,
		StateBytes:               100,
		StateHash:                HexHash{1, 2, 3},
		PebbleFormatMajorVersion: pebbledb.FormatMajorVersion,
		Files: []File{
			{Path: "flatkv/account/MANIFEST-000001", Size: 10},
			{Path: "flatkv/code/MANIFEST-000001", Size: 10},
			{Path: "flatkv/storage/000005.sst", Size: 0},
			{Path: "flatkv/misc/OPTIONS-000003", Size: 1},
		},
		ProducerVersion: "test",
	}
}

func TestManifestRoundTrip(t *testing.T) {
	m := validManifest()
	data, err := EncodeManifest(m)
	require.NoError(t, err)

	got, err := DecodeManifest(data)
	require.NoError(t, err)
	require.Equal(t, m, got)
}

func TestManifestValidateRejects(t *testing.T) {
	for name, change := range map[string]func(m *Manifest){
		"format version":      func(m *Manifest) { m.FormatVersion++ },
		"empty chain ID":      func(m *Manifest) { m.ChainID = "" },
		"zero height":         func(m *Manifest) { m.Height = 0 },
		"negative rows":       func(m *Manifest) { m.RowCount = -1 },
		"newer pebble format": func(m *Manifest) { m.PebbleFormatMajorVersion = pebbledb.FormatMajorVersion + 1 },
		"unsupported pebble format": func(m *Manifest) {
			m.PebbleFormatMajorVersion = pebble.FormatMinSupported - 1
		},
		"negative size":     func(m *Manifest) { m.Files[0].Size = -1 },
		"duplicate file":    func(m *Manifest) { m.Files = append(m.Files, m.Files[0]) },
		"missing database":  func(m *Manifest) { m.Files = m.Files[1:] },
		"unknown database":  func(m *Manifest) { m.Files[0].Path = "flatkv/other/MANIFEST-000001" },
		"nested path":       func(m *Manifest) { m.Files[0].Path = "flatkv/account/dir/MANIFEST-000001" },
		"parent path":       func(m *Manifest) { m.Files[0].Path = "flatkv/account/.." },
		"lock file":         func(m *Manifest) { m.Files[0].Path = "flatkv/account/LOCK" },
		"outside flatkv":    func(m *Manifest) { m.Files[0].Path = "proof.bin" },
		"absolute path":     func(m *Manifest) { m.Files[0].Path = "/flatkv/account/MANIFEST-000001" },
		"empty file name":   func(m *Manifest) { m.Files[0].Path = "flatkv/account/" },
		"negative bytes":    func(m *Manifest) { m.StateBytes = -1 },
		"negative height":   func(m *Manifest) { m.Height = -7 },
		"no files":          func(m *Manifest) { m.Files = nil },
		"current directory": func(m *Manifest) { m.Files[0].Path = "flatkv/account/." },
	} {
		t.Run(name, func(t *testing.T) {
			m := validManifest()
			change(m)
			require.Error(t, m.Validate())
		})
	}
}

func TestManifestValidateAcceptsAnOlderPebbleFormat(t *testing.T) {
	m := validManifest()
	m.PebbleFormatMajorVersion = pebble.FormatMinSupported

	require.NoError(t, m.Validate())
}

func TestDecodeManifestRejects(t *testing.T) {
	data, err := EncodeManifest(validManifest())
	require.NoError(t, err)
	var fields map[string]any
	require.NoError(t, json.Unmarshal(data, &fields))
	fields["extra"] = 1
	withExtra, err := json.Marshal(fields)
	require.NoError(t, err)

	for name, input := range map[string][]byte{
		"unknown field": withExtra,
		"trailing data": append(append([]byte{}, data...), []byte(" {}")...),
		"short hash":    []byte(`{"state_hash":"0102"}`),
		"not json":      []byte("manifest"),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := DecodeManifest(input)
			require.Error(t, err)
		})
	}
}
