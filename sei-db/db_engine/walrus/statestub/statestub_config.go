package statestub

import (
	"fmt"
	"regexp"
)

// The permitted shape of an instance name: it becomes a metric attribute value, so it is restricted to
// characters safe for label values.
var nameRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// Config configures a StateStub.
type Config struct {
	// The directory where the store keeps its data and stages checkpoints.
	Path string

	// A short identifier for this instance, used to distinguish its metrics from those of other instances in
	// the same process. Required; must match [a-zA-Z0-9_-]+.
	Name string

	// The name of the store whose changesets this instance applies. CommitBlock rejects a changeset carrying
	// any other name.
	StoreName string

	// Whether this instance stops recording metrics. Metrics are on by default, so a caller has to ask for
	// silence rather than remember to ask for data.
	DisableMetrics bool
}

// DefaultConfig returns a default configuration for the store at path, identified by name, holding the store
// named storeName.
func DefaultConfig(path string, name string, storeName string) *Config {
	return &Config{
		Path:           path,
		Name:           name,
		StoreName:      storeName,
		DisableMetrics: false,
	}
}

// Validate the configuration, returning nil if valid, or an error describing the problem if invalid.
func (c *Config) Validate() error {
	if c.Path == "" {
		return fmt.Errorf("path is required")
	}
	if !nameRegex.MatchString(c.Name) {
		return fmt.Errorf("name %q is required and must match %s", c.Name, nameRegex.String())
	}
	if c.StoreName == "" {
		return fmt.Errorf("store name is required")
	}
	return nil
}
