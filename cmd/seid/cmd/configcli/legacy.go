package configcli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

// legacyFiles are the configuration files a node keeps today, in the order they are merged.
//
// app.toml then config.toml, matching the order the boot reads them, so a key both files carry
// resolves here the way it resolves on the running node.
var legacyFiles = []string{"app.toml", "config.toml"}

// LegacySource reads a node's existing configuration files.
//
// Only the files. Environment variables and flags are deliberately absent, because adoption reports
// those rather than writing them: a variable sits above the file, so folding its value in would
// change nothing while it is set and would change the node's behaviour the day it is unset. A source
// that included them could not tell the two apart.
func LegacySource(home string) (Source, error) {
	v := viper.New()
	found := 0

	for _, name := range legacyFiles {
		path := filepath.Join(home, "config", name)
		if _, err := os.Stat(path); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		v.SetConfigFile(path)
		if err := v.MergeInConfig(); err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		found++
	}

	if found == 0 {
		return nil, fmt.Errorf("no configuration to adopt: neither %s nor %s is in %s. Generate a "+
			"file from defaults instead", legacyFiles[0], legacyFiles[1],
			filepath.Join(home, "config"))
	}
	return v, nil
}
