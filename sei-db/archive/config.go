package archive

import (
	"fmt"
	"regexp"
	"time"

	"github.com/ClickHouse/ch-go"
)

// Config locates the ClickHouse archive.
type Config struct {
	// Addr is the ClickHouse native-protocol address, host:port.
	Addr string
	// Database is the archive's database. It must be a plain identifier: letters, digits and
	// underscores.
	Database string
	User     string
	Password string
}

var identifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func (c Config) validate() error {
	if !identifier.MatchString(c.Database) {
		return fmt.Errorf("archive database %q is not a plain identifier", c.Database)
	}
	return nil
}

// writeTimeout bounds one attempt to write a batch.
const writeTimeout = time.Minute

// options returns the connection options for database.
func (c Config) options(database string) ch.Options {
	return ch.Options{
		Address:     c.Addr,
		Database:    database,
		User:        c.User,
		Password:    c.Password,
		Compression: ch.CompressionLZ4,
		ClientName:  "sei-archive",
		// ClickHouse answers an INSERT only once its parts are written, which can take longer than
		// the client's default 3s; each write attempt is bounded by its context instead.
		ReadTimeout: writeTimeout,
	}
}
