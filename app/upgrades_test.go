package app

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestOverrideList(t *testing.T) {
	defaultList := upgradesList
	tests := []struct {
		name         string
		envValue     string
		expectedList []string
	}{
		{
			name:         "UPGRADE_VERSION_LIST not set",
			envValue:     "",
			expectedList: defaultList,
		},
		{
			name:         "UPGRADE_VERSION_LIST set with single value",
			envValue:     "2.0.0",
			expectedList: []string{"2.0.0"},
		},
		{
			name:         "UPGRADE_VERSION_LIST set with multiple values",
			envValue:     "2.0.0,2.1.0,2.2.0",
			expectedList: []string{"2.0.0", "2.1.0", "2.2.0"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envValue != "" {
				t.Setenv("UPGRADE_VERSION_LIST", tt.envValue)
			}
			// reset upgrades list before each test
			upgradesList = defaultList

			overrideList()

			assert.True(t, reflect.DeepEqual(tt.expectedList, upgradesList), "Expected %v but got %v", tt.expectedList, upgradesList)
		})
	}
}

func TestParseUpgradesList(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "empty string",
			input:    "",
			expected: []string{},
		},
		{
			name:  "comma separated",
			input: "v3.0.0,v1.0.2beta,v2.0.29beta",
			expected: []string{
				"v1.0.2beta",
				"v2.0.29beta",
				"v3.0.0",
			},
		},
		{
			name:  "comma separated double digit",
			input: "v3.11.0,v3.0.0,v1.0.2beta,v2.0.29beta,,v3.10.0",
			expected: []string{
				"v1.0.2beta",
				"v2.0.29beta",
				"v3.0.0",
				"v3.10.0",
				"v3.11.0",
			},
		},
		{
			name:  "newline separated",
			input: "v3.0.0\nv1.0.2beta\nv2.0.29beta",
			expected: []string{
				"v1.0.2beta",
				"v2.0.29beta",
				"v3.0.0",
			},
		},
		{
			name:  "mixed comma and newline separators",
			input: "v3.0.0,v1.0.2beta\nv2.0.29beta",
			expected: []string{
				"v1.0.2beta",
				"v2.0.29beta",
				"v3.0.0",
			},
		},
		{
			name:  "consecutive separators are ignored",
			input: "v3.0.0,,\n\nv1.0.2beta,\n,v2.0.29beta",
			expected: []string{
				"v1.0.2beta",
				"v2.0.29beta",
				"v3.0.0",
			},
		},
		{
			name:  "already sorted input stays sorted",
			input: "1.0.2beta,1.0.3beta,1.0.4beta",
			expected: []string{
				"1.0.2beta",
				"1.0.3beta",
				"1.0.4beta",
			},
		},
		{
			name:  "reverse sorted input gets sorted",
			input: "v6.4.0,v5.0.0,v3.0.0,1.0.2beta",
			expected: []string{
				"1.0.2beta",
				"v3.0.0",
				"v5.0.0",
				"v6.4.0",
			},
		},
		{
			name:  "prerelease tags sort before release",
			input: "v4.0.0-evm-devnet,v3.9.0,v4.0.1-evm-devnet",
			expected: []string{
				"v3.9.0",
				"v4.0.0-evm-devnet",
				"v4.0.1-evm-devnet",
			},
		},
		{
			name:  "mixed v-prefix and no prefix",
			input: "v3.0.9,3.0.8,3.0.7",
			expected: []string{
				"3.0.7",
				"3.0.8",
				"v3.0.9",
			},
		},
		{
			name:  "postfix prerelease versions",
			input: "1.2.2beta-postfix,1.0.7beta-postfix,1.1.2beta-internal",
			expected: []string{
				"1.0.7beta-postfix",
				"1.1.2beta-internal",
				"1.2.2beta-postfix",
			},
		},
		{
			name:     "single entry",
			input:    "v6.4.0",
			expected: []string{"v6.4.0"},
		},
		{
			name:  "large mixed list newline separated",
			input: "v6.0.0\nv5.0.0\n1.0.2beta\nv3.0.0\n2.0.29beta\nv4.0.0-evm-devnet\n1.1.0beta",
			expected: []string{
				"1.0.2beta",
				"1.1.0beta",
				"2.0.29beta",
				"v3.0.0",
				"v4.0.0-evm-devnet",
				"v5.0.0",
				"v6.0.0",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseUpgradesList(tt.input)
			if len(got) == 0 && len(tt.expected) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("parseUpgradesList(%q)\n  got:  %v\n  want: %v", tt.input, got, tt.expected)
			}
		})
	}
}
