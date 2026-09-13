package config

import (
	"bytes"
	"strings"
	"testing"
	"text/template"

	"github.com/stretchr/testify/require"
)

// renderedAssignments returns the keys StateCommitConfigTemplate assigns when it
// is rendered with the in-code defaults, one name per entry. Comments are dropped,
// so a key the template only discusses in prose is not mistaken for one it writes.
func renderedAssignments(t *testing.T) []string {
	t.Helper()

	tmpl, err := template.New("sc").Parse(StateCommitConfigTemplate)
	require.NoError(t, err)

	var rendered bytes.Buffer
	require.NoError(t, tmpl.Execute(&rendered, struct{ StateCommit StateCommitConfig }{
		StateCommit: DefaultStateCommitConfig(),
	}))

	var keys []string
	for _, line := range strings.Split(rendered.String(), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "[") {
			continue
		}
		key, _, ok := strings.Cut(line, "=")
		require.True(t, ok, "unexpected non-assignment line in the rendered template: %q", line)
		keys = append(keys, strings.TrimSpace(key))
	}
	return keys
}
