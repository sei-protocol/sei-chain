package app

import (
	"testing"

	"github.com/sei-protocol/sei-db/config"
	"github.com/stretchr/testify/assert"
)

func TestNewDefaultConfig(t *testing.T) {
	// Make sure when adding a new default config, it should apply to SeiDB during initialization
	appOpts := TestAppOpts{
		useSC: config.DefaultStateCommitConfig().Enable,
		useSS: config.DefaultStateStoreConfig().Enable,
	}
	scConfig := parseSCConfigs(appOpts)
	ssConfifg := parseSSConfigs(appOpts)
	assert.Equal(t, scConfig, config.DefaultStateCommitConfig())
	assert.Equal(t, ssConfifg, config.DefaultStateStoreConfig())
}
