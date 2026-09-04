package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/upgradetest"
	"github.com/stretchr/testify/require"
)

func TestRunPrintsCurrentBoundaryFields(t *testing.T) {
	boundary, err := upgradetest.Current()
	require.NoError(t, err)
	tag, err := boundary.Tag()
	require.NoError(t, err)
	file, err := boundary.TestFile()
	require.NoError(t, err)

	for _, tc := range []struct {
		request string
		want    string
	}{
		{request: "from", want: boundary.From + "\n"},
		{request: "to", want: boundary.To + "\n"},
		{request: "tag", want: tag + "\n"},
		{request: "file", want: file + "\n"},
	} {
		t.Run(tc.request, func(t *testing.T) {
			var output bytes.Buffer
			require.NoError(t, run([]string{tc.request}, &output))
			require.Equal(t, tc.want, output.String())
		})
	}
}

func TestRunRejectsUnknownField(t *testing.T) {
	err := run([]string{"directory"}, &bytes.Buffer{})
	require.ErrorContains(t, err, "want boundary, from, to, tag or file")
}

func TestRunToPrintsAShippedUpgradeName(t *testing.T) {
	var output bytes.Buffer
	require.NoError(t, run([]string{"to"}, &output))
	printed := strings.TrimSuffix(output.String(), "\n")
	require.Equal(t, printed, strings.TrimSpace(printed),
		"boundary to padded the upgrade name with whitespace")
	require.Contains(t, app.ReleaseUpgrades(), printed)
}
