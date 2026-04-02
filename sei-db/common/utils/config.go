package utils

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Config is implemented by any configuration struct that supports validation.
type Config interface {
	Validate() error
}

// StringifyConfig returns the config as human-readable, multi-line JSON.
func StringifyConfig(cfg Config) (string, error) {
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// LoadConfigFromFile decodes a JSON config file on top of the provided defaults
// struct. The caller should pass a pointer to a struct pre-populated with
// default values; file values are overlaid on top. Unknown JSON keys cause an
// error. After decoding, Validate() is called on the result.
func LoadConfigFromFile(path string, defaults Config) error {
	//nolint:gosec // G304 - path comes from CLI arg, filepath.Clean used to mitigate traversal
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return fmt.Errorf("open config file: %w", err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			fmt.Printf("failed to close config file: %v\n", err)
		}
	}()

	dec := json.NewDecoder(f)
	dec.DisallowUnknownFields()
	if err := dec.Decode(defaults); err != nil {
		return fmt.Errorf("decode config: %w", err)
	}
	if err := defaults.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}
	return nil
}
