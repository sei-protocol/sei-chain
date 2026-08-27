package runner_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// Every YAML suite must parse into the runner's schema and name a verifier type
// the runner implements. Without this, a malformed suite only surfaces on a
// cluster run, which is the slowest place to find a typo.
func TestYAMLSuitesParse(t *testing.T) {
	files, err := filepath.Glob("../*/*.yaml")
	require.NoError(t, err)
	require.NotEmpty(t, files)

	type input struct {
		Cmd  string `yaml:"cmd"`
		Env  string `yaml:"env,omitempty"`
		Node string `yaml:"node,omitempty"`
	}
	type verifier struct {
		Type   string `yaml:"type"`
		Expr   string `yaml:"expr"`
		Result string `yaml:"result,omitempty"`
	}
	type testCase struct {
		Name      string     `yaml:"name"`
		Inputs    []input    `yaml:"inputs"`
		Verifiers []verifier `yaml:"verifiers"`
	}

	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			raw, err := os.ReadFile(file)
			require.NoError(t, err)

			var cases []testCase
			require.NoError(t, yaml.Unmarshal(raw, &cases))
			require.NotEmpty(t, cases)

			for _, tc := range cases {
				require.NotEmpty(t, tc.Name)
				for i, in := range tc.Inputs {
					require.NotEmpty(t, in.Cmd, "input %d has no cmd", i)
				}
				for i, v := range tc.Verifiers {
					require.Contains(t, []string{"eval", "regex"}, v.Type,
						"verifier %d has unknown type", i)
					require.NotEmpty(t, v.Expr, "verifier %d has no expr", i)
					if v.Type == "regex" {
						require.NotEmpty(t, v.Result,
							"regex verifier %d needs the env var to match against", i)
					}
				}
			}
		})
	}
}
