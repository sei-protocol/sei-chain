package hashlog

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConfigValidateDefault(t *testing.T) {
	require.NoError(t, DefaultHashLoggerConfig("/tmp/hashlog", "v1.0.0").Validate())
}

func TestConfigValidateEmptyPath(t *testing.T) {
	c := DefaultHashLoggerConfig("", "v1.0.0")
	require.ErrorContains(t, c.Validate(), "path is required")
}

func TestConfigValidateEmptyVersion(t *testing.T) {
	c := DefaultHashLoggerConfig("/tmp/hashlog", "")
	require.ErrorContains(t, c.Validate(), "version is required")
}

func TestConfigValidateZeroMaxDiskSizeIsAllowed(t *testing.T) {
	// MaxDiskSize == 0 disables the disk-size cap rather than being an error.
	c := DefaultHashLoggerConfig("/tmp/hashlog", "v1.0.0")
	c.MaxDiskSize = 0
	require.NoError(t, c.Validate())
}

func TestConfigValidateZeroMaxBufferedBlocks(t *testing.T) {
	c := DefaultHashLoggerConfig("/tmp/hashlog", "v1.0.0")
	c.MaxBufferedBlocks = 0
	require.ErrorContains(t, c.Validate(), "max buffered blocks")
}

func TestConfigValidateEmptyHashTypes(t *testing.T) {
	c := DefaultHashLoggerConfig("/tmp/hashlog", "v1.0.0")
	c.HashTypes = nil
	c.DisableChangesetHashing = true
	require.ErrorContains(t, c.Validate(), "at least one hash type")
}

func TestConfigValidateDuplicateHashTypes(t *testing.T) {
	c := DefaultHashLoggerConfig("/tmp/hashlog", "v1.0.0")
	c.HashTypes = []string{"root", "root"}
	require.ErrorContains(t, c.Validate(), "duplicate hash type")
}

func TestConfigValidateIllegalHashType(t *testing.T) {
	c := DefaultHashLoggerConfig("/tmp/hashlog", "v1.0.0")
	c.HashTypes = []string{"a,b"}
	require.ErrorContains(t, c.Validate(), "illegal characters")
}

func TestConfigValidateReservedChangesetHashType(t *testing.T) {
	c := DefaultHashLoggerConfig("/tmp/hashlog", "v1.0.0")
	c.HashTypes = []string{ChangesetHashType, "root"}
	require.ErrorContains(t, c.Validate(), "reserved")
}

func TestConfigValidateReservedChangesetHashTypeRejectedEvenWhenDisabled(t *testing.T) {
	// The changeset name stays reserved even with changeset hashing disabled, so a config can never silently mean
	// different columns depending on the flag.
	c := DefaultHashLoggerConfig("/tmp/hashlog", "v1.0.0")
	c.HashTypes = []string{ChangesetHashType, "root"}
	c.DisableChangesetHashing = true
	require.ErrorContains(t, c.Validate(), "reserved")
}

func TestConfigValidateChangesetHashTypeDisabled(t *testing.T) {
	c := DefaultHashLoggerConfig("/tmp/hashlog", "v1.0.0")
	c.HashTypes = []string{"flatKV", "root"}
	c.DisableChangesetHashing = true // disabling changeset hashing is allowed
	require.NoError(t, c.Validate())
}
