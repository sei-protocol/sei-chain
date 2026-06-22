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

func TestConfigValidateZeroMaxDiskSize(t *testing.T) {
	c := DefaultHashLoggerConfig("/tmp/hashlog", "v1.0.0")
	c.MaxDiskSize = 0
	require.ErrorContains(t, c.Validate(), "max disk size")
}

func TestConfigValidateEmptyHashTypes(t *testing.T) {
	c := DefaultHashLoggerConfig("/tmp/hashlog", "v1.0.0")
	c.HashTypes = nil
	c.DiffHashType = ""
	require.ErrorContains(t, c.Validate(), "at least one hash type")
}

func TestConfigValidateDuplicateHashTypes(t *testing.T) {
	c := DefaultHashLoggerConfig("/tmp/hashlog", "v1.0.0")
	c.HashTypes = []string{"diff", "diff"}
	require.ErrorContains(t, c.Validate(), "duplicate hash type")
}

func TestConfigValidateIllegalHashType(t *testing.T) {
	c := DefaultHashLoggerConfig("/tmp/hashlog", "v1.0.0")
	c.HashTypes = []string{"a,b"}
	c.DiffHashType = ""
	require.ErrorContains(t, c.Validate(), "illegal characters")
}

func TestConfigValidateDiffHashTypeNotInHashTypes(t *testing.T) {
	c := DefaultHashLoggerConfig("/tmp/hashlog", "v1.0.0")
	c.HashTypes = []string{"flatKV", "root"}
	c.DiffHashType = "diff"
	require.ErrorContains(t, c.Validate(), "diff hash type")
}

func TestConfigValidateDiffHashTypeDisabled(t *testing.T) {
	c := DefaultHashLoggerConfig("/tmp/hashlog", "v1.0.0")
	c.HashTypes = []string{"flatKV", "root"}
	c.DiffHashType = "" // disabling diff hashing is allowed
	require.NoError(t, c.Validate())
}
