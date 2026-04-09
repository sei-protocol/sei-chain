package keymap

import (
	"fmt"
	"os"
	"path"

	"log/slog"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/metrics"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/util"
)

// keymapBuilders contains builder functions for all supported keymap types.
var keymapBuilders = map[KeymapType]BuildKeymap{
	MemKeymapType:           NewMemKeymap,
	LevelDBKeymapType:       NewLevelDBKeymap,
	UnsafeLevelDBKeymapType: NewUnsafeLevelDBKeymap,
	PebbleKeymapType:        NewPebbleKeymap,
	UnsafePebbleKeymapType:  NewUnsafePebbleKeymap,
}

// FindKeymapLocation looks for a table's keymap directory in the provided root paths.
func FindKeymapLocation(
	rootPaths []string,
	tableName string,
) (keymapDirectory string, keymapInitialized bool, keymapTypeFile *KeymapTypeFile, error error) {

	if len(rootPaths) == 0 {
		return "", false, nil,
			fmt.Errorf("no segment paths provided for keymap search")
	}

	potentialKeymapDirectories := make([]string, len(rootPaths))
	for i, rootPath := range rootPaths {
		potentialKeymapDirectories[i] = path.Join(rootPath, tableName, KeymapDirectoryName)
	}

	for _, directory := range potentialKeymapDirectories {
		exists, err := util.Exists(directory)
		if err != nil {
			return "", false, nil,
				fmt.Errorf("error checking for keymap type file: %w", err)
		}
		if exists {
			if keymapDirectory != "" {
				return "", false, nil,
					fmt.Errorf("multiple keymap directories found: %s and %s", keymapDirectory, directory)
			}

			keymapDirectory = directory
			keymapTypeFile, err = LoadKeymapTypeFile(directory)
			if err != nil {
				return "", false, nil,
					fmt.Errorf("error loading keymap type file: %w", err)
			}

			initializedExists, err := util.Exists(path.Join(keymapDirectory, KeymapInitializedFileName))
			if err != nil {
				return "", false, nil,
					fmt.Errorf("error checking for keymap initialized file: %w", err)
			}
			if initializedExists {
				keymapInitialized = true
			}
		}
	}

	return keymapDirectory, keymapInitialized, keymapTypeFile, nil
}

// OpenOrCreate creates or opens a keymap based on the provided parameters.
func OpenOrCreate(
	logger *slog.Logger,
	keymapType KeymapType,
	paths []string,
	tableName string,
	doubleWriteProtection bool,
	m *metrics.LittDBMetrics,
) (kmap Keymap, keymapPath string, keymapTypeFile *KeymapTypeFile, requiresReload bool, err error) {

	builderForConfiguredType, ok := keymapBuilders[keymapType]
	if !ok {
		return nil, "", nil, false,
			fmt.Errorf("unsupported keymap type: %v", keymapType)
	}

	keymapDirectory, keymapInitialized, keymapTypeFile, err := FindKeymapLocation(paths, tableName)
	if err != nil {
		return nil, "", nil, false,
			fmt.Errorf("error finding keymap location: %w", err)
	}

	if keymapTypeFile != nil && !keymapInitialized {
		logger.Warn(fmt.Sprintf("incomplete keymap initialization detected. Deleting keymap directory: %s",
			keymapDirectory))

		err := os.RemoveAll(keymapDirectory)
		if err != nil {
			return nil, "", nil, false,
				fmt.Errorf("error deleting keymap directory: %w", err)
		}

		keymapTypeFile = nil
		keymapDirectory = ""
	}

	newKeymap := false
	if keymapTypeFile == nil {
		newKeymap = true

		keymapDirectory = path.Join(paths[0], tableName, KeymapDirectoryName)
		keymapTypeFile = NewKeymapTypeFile(keymapDirectory, keymapType)

		err := os.MkdirAll(keymapDirectory, 0755) //nolint:gosec
		if err != nil {
			return nil, "", nil, false,
				fmt.Errorf("error creating keymap directory: %w", err)
		}

		err = keymapTypeFile.Write()
		if err != nil {
			return nil, "", nil, false,
				fmt.Errorf("error writing keymap type file: %w", err)
		}

	} else {
		if keymapType != keymapTypeFile.Type() {
			keymapTypeFile = nil

			err = os.RemoveAll(keymapDirectory)
			if err != nil {
				return nil, "", nil, false,
					fmt.Errorf("error deleting keymap files: %w", err)
			}

			err = os.MkdirAll(keymapDirectory, 0755) //nolint:gosec
			if err != nil {
				return nil, "", nil, false,
					fmt.Errorf("error creating keymap directory: %w", err)
			}
			keymapTypeFile = NewKeymapTypeFile(keymapDirectory, keymapType)
			err = keymapTypeFile.Write()
			if err != nil {
				return nil, "", nil, false,
					fmt.Errorf("error writing keymap type file: %w", err)
			}
		}
	}

	keymapDataDirectory := path.Join(keymapDirectory, KeymapDataDirectoryName)
	kmap, requiresReload, err = builderForConfiguredType(logger, keymapDataDirectory, doubleWriteProtection, m)
	if err != nil {
		return nil, "", nil, false,
			fmt.Errorf("error building keymap: %w", err)
	}

	if !requiresReload {
		keymapInitializedFile := path.Join(keymapDirectory, KeymapInitializedFileName)
		f, err := os.Create(keymapInitializedFile) //nolint:gosec
		if err != nil {
			return nil, "", nil, false,
				fmt.Errorf("failed to create keymap initialized file: %v", err)
		}
		err = f.Close()
		if err != nil {
			return nil, "", nil, false,
				fmt.Errorf("failed to close keymap initialized file: %v", err)
		}
	}

	return kmap, keymapDirectory, keymapTypeFile, requiresReload || newKeymap, nil
}
