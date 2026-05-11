package historical

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gocql/gocql"
)

const (
	defaultScyllaConsistency = "local_quorum"
	defaultScyllaTimeout     = 2 * time.Second
	defaultScyllaNumConns    = 4

	// VersionBucketCount spreads monotonically increasing block-version markers
	// across a bounded set of partitions while keeping LastVersion cheap.
	VersionBucketCount = 64
)

type ScyllaConfig struct {
	Hosts          []string
	Keyspace       string
	Username       string
	Password       string
	Datacenter     string
	Consistency    string
	Timeout        time.Duration
	ConnectTimeout time.Duration
	NumConns       int
}

func (c *ScyllaConfig) ApplyDefaults() {
	if c.Consistency == "" {
		c.Consistency = defaultScyllaConsistency
	}
	if c.Timeout == 0 {
		c.Timeout = defaultScyllaTimeout
	}
	if c.ConnectTimeout == 0 {
		c.ConnectTimeout = defaultScyllaTimeout
	}
	if c.NumConns == 0 {
		c.NumConns = defaultScyllaNumConns
	}
}

func (c *ScyllaConfig) Validate() error {
	if len(c.Hosts) == 0 {
		return fmt.Errorf("scylla/cassandra hosts are required")
	}
	for _, host := range c.Hosts {
		if strings.TrimSpace(host) == "" {
			return fmt.Errorf("scylla/cassandra hosts must not contain blanks")
		}
	}
	if strings.TrimSpace(c.Keyspace) == "" {
		return fmt.Errorf("scylla/cassandra keyspace is required")
	}
	if c.Password != "" && c.Username == "" {
		return fmt.Errorf("scylla/cassandra username is required when password is set")
	}
	if _, err := parseConsistency(c.Consistency); err != nil {
		return err
	}
	if c.Timeout < 0 {
		return fmt.Errorf("scylla/cassandra timeout must be non-negative")
	}
	if c.ConnectTimeout < 0 {
		return fmt.Errorf("scylla/cassandra connect timeout must be non-negative")
	}
	if c.NumConns < 0 {
		return fmt.Errorf("scylla/cassandra num conns must be non-negative")
	}
	return nil
}

func NewScyllaReader(cfg ScyllaConfig) (Reader, error) {
	session, err := OpenScyllaSession(cfg)
	if err != nil {
		return nil, err
	}
	return &scyllaReader{session: session}, nil
}

func OpenScyllaSession(cfg ScyllaConfig) (*gocql.Session, error) {
	cfg.ApplyDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	consistency, err := parseConsistency(cfg.Consistency)
	if err != nil {
		return nil, err
	}

	cluster := gocql.NewCluster(cfg.Hosts...)
	cluster.Keyspace = cfg.Keyspace
	cluster.Consistency = consistency
	cluster.Timeout = cfg.Timeout
	cluster.ConnectTimeout = cfg.ConnectTimeout
	cluster.NumConns = cfg.NumConns
	if cfg.Username != "" {
		cluster.Authenticator = gocql.PasswordAuthenticator{
			Username: cfg.Username,
			Password: cfg.Password,
		}
	}
	if cfg.Datacenter != "" {
		cluster.PoolConfig.HostSelectionPolicy = gocql.TokenAwareHostPolicy(
			gocql.DCAwareRoundRobinPolicy(cfg.Datacenter),
		)
	}

	session, err := cluster.CreateSession()
	if err != nil {
		return nil, fmt.Errorf("open scylla/cassandra session: %w", err)
	}
	return session, nil
}

type scyllaReader struct {
	session *gocql.Session
}

var _ Reader = (*scyllaReader)(nil)

func (r *scyllaReader) Close() error {
	r.session.Close()
	return nil
}

func (r *scyllaReader) LastVersion(ctx context.Context) (int64, error) {
	var maxVersion int64
	for bucket := 0; bucket < VersionBucketCount; bucket++ {
		var version int64
		err := r.session.Query(selectLatestVersionCQL, bucket).WithContext(ctx).Scan(&version)
		if err != nil {
			if err == gocql.ErrNotFound {
				continue
			}
			return 0, fmt.Errorf("read latest scylla/cassandra version bucket %d: %w", bucket, err)
		}
		if version > maxVersion {
			maxVersion = version
		}
	}
	return maxVersion, nil
}

func (r *scyllaReader) Has(ctx context.Context, storeName string, key []byte, targetVersion int64) (bool, error) {
	var deleted bool
	err := r.session.Query(hasLookupCQL, storeName, key, targetVersion).WithContext(ctx).Scan(&deleted)
	if err != nil {
		if err == gocql.ErrNotFound {
			return false, nil
		}
		return false, fmt.Errorf("scylla/cassandra has lookup: %w", err)
	}
	return !deleted, nil
}

func (r *scyllaReader) Get(ctx context.Context, storeName string, key []byte, targetVersion int64) (Value, error) {
	var (
		version int64
		bz      []byte
		deleted bool
	)
	err := r.session.Query(getLookupCQL, storeName, key, targetVersion).WithContext(ctx).Scan(&version, &bz, &deleted)
	if err != nil {
		if err == gocql.ErrNotFound {
			return Value{}, ErrNotFound
		}
		return Value{}, fmt.Errorf("scylla/cassandra get lookup: %w", err)
	}
	if deleted {
		return Value{}, ErrNotFound
	}
	return Value{Bytes: bz, Version: version}, nil
}

func (r *scyllaReader) BatchGet(ctx context.Context, targetVersion int64, lookups []Lookup) (map[Lookup]Value, error) {
	out := make(map[Lookup]Value, len(lookups))
	for _, lookup := range lookups {
		value, err := r.Get(ctx, lookup.StoreName, []byte(lookup.Key), targetVersion)
		if err != nil {
			if err == ErrNotFound {
				continue
			}
			return nil, err
		}
		out[lookup] = value
	}
	return out, nil
}

func VersionBucket(version int64) int {
	if version < 0 {
		version = -version
	}
	return int(version % VersionBucketCount)
}

func parseConsistency(name string) (gocql.Consistency, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "one":
		return gocql.One, nil
	case "local_one":
		return gocql.LocalOne, nil
	case "quorum":
		return gocql.Quorum, nil
	case "", "local_quorum":
		return gocql.LocalQuorum, nil
	case "all":
		return gocql.All, nil
	default:
		return gocql.Any, fmt.Errorf("unsupported scylla/cassandra consistency %q", name)
	}
}

const selectLatestVersionCQL = `
SELECT version
FROM state_versions
WHERE bucket = ?
LIMIT 1`

const hasLookupCQL = `
SELECT deleted
FROM state_mutations
WHERE store_name = ? AND state_key = ? AND version <= ?
ORDER BY version DESC
LIMIT 1`

const getLookupCQL = `
SELECT version, value, deleted
FROM state_mutations
WHERE store_name = ? AND state_key = ? AND version <= ?
ORDER BY version DESC
LIMIT 1`
