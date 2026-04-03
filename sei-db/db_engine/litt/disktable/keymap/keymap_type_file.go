package keymap

import (
	"fmt"
	"os"
	"path"

	"github.com/Layr-Labs/eigenda/litt/util"
)

// KeymapTypeFileName is the name of the file that contains the keymap type.
const KeymapTypeFileName = "keymap-type.txt"

// KeymapTypeFile is a text file that contains the name of the keymap type. This is used to determine if the keymap
// needs to reload when littDB is restarted, or if the data structures in the keymap directory are still valid.
type KeymapTypeFile struct {
	// keymapPath is the path to the keymap directory.
	keymapPath string

	// KeymapType is the type of the keymap currently stored in the keymap directory.
	keymapType KeymapType
}

// KeymapFileExists checks if the keymap type file exists in the target directory.
func KeymapFileExists(keymapPath string) (bool, error) {
	return util.Exists(path.Join(keymapPath, KeymapTypeFileName))
}

// NewKeymapTypeFile creates a new KeymapTypeFile.
func NewKeymapTypeFile(keymapPath string, keymapType KeymapType) *KeymapTypeFile {
	return &KeymapTypeFile{
		keymapPath: keymapPath,
		keymapType: keymapType,
	}
}

// LoadKeymapTypeFile loads the keymap type from the keymap directory.
func LoadKeymapTypeFile(keymapPath string) (*KeymapTypeFile, error) {
	filePath := path.Join(keymapPath, KeymapTypeFileName)

	if err := util.ErrIfNotExists(filePath); err != nil {
		return nil, fmt.Errorf("keymap type file does not exist: %v", filePath)
	}

	fileContents, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("unable to read keymap type file: %v", err)
	}

	var keymapType KeymapType
	switch string(fileContents) {
	case MemKeymapType:
		keymapType = MemKeymapType
	case LevelDBKeymapType:
		keymapType = LevelDBKeymapType
	case UnsafeLevelDBKeymapType:
		keymapType = UnsafeLevelDBKeymapType
	default:
		return nil, fmt.Errorf("unknown keymap type: %s", string(fileContents))
	}

	return &KeymapTypeFile{
		keymapPath: keymapPath,
		keymapType: keymapType,
	}, nil
}

// Type returns the type of the keymap.
func (k *KeymapTypeFile) Type() KeymapType {
	return k.keymapType
}

// Write writes the keymap type to the keymap directory.
func (k *KeymapTypeFile) Write() error {
	filePath := path.Join(k.keymapPath, KeymapTypeFileName)

	exists, _, err := util.ErrIfNotWritableFile(filePath)
	if err != nil {
		return fmt.Errorf("unable to open keymap type file: %v", err)
	}

	if exists {
		return fmt.Errorf("keymap type file already exists: %v", filePath)
	}

	keymapFile, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("unable to create keymap type file: %v", err)
	}

	_, err = keymapFile.WriteString(string(k.keymapType))
	if err != nil {
		return fmt.Errorf("unable to write keymap type file: %v", err)
	}

	err = keymapFile.Close()
	if err != nil {
		return fmt.Errorf("unable to close keymap type file: %v", err)
	}

	return nil
}

// Delete deletes the keymap type file.
func (k *KeymapTypeFile) Delete() error {
	exists, err := util.Exists(path.Join(k.keymapPath, KeymapTypeFileName))
	if err != nil {
		return fmt.Errorf("error checking for keymap type file: %w", err)
	}
	if !exists {
		return nil
	}

	err = os.Remove(path.Join(k.keymapPath, KeymapTypeFileName))
	if err != nil {
		return fmt.Errorf("unable to delete keymap type file: %v", err)
	}
	return nil
}
