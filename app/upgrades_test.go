package app

import (
	"os"
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
				os.Setenv("UPGRADE_VERSION_LIST", tt.envValue)
				defer os.Unsetenv("UPGRADE_VERSION_LIST")
			}
			// reset upgrades list before each test
			upgradesList = defaultList

			overrideList()

			assert.True(t, reflect.DeepEqual(tt.expectedList, upgradesList), "Expected %v but got %v", tt.expectedList, upgradesList)
		})
	}
}
