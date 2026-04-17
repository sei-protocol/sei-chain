package migration

import (
	"errors"
	"fmt"
	"sort"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/seilog"
)

var logger = seilog.NewLogger("db", "state-db", "sc", "migration")

// Handles the migration from one database to another. Routes reads and writes during the course of the migration.
type MigrationManager struct {

	// For reading values out of the old database.
	oldDBReader DBReader

	// For writing values to the old database.
	oldDBWriter DBWriter

	// For reading values out of the new database.
	newDBReader DBReader

	// For writing values to the new database.
	newDBWriter DBWriter

	// For iterating through key-value pairs to migrate in the old database.
	iterator MigrationIterator

	// The boundary of the migration. All keys to the left of the boundary (are considered migrated.
	boundary MigrationBoundary

	// The number of key-value pairs to migrate after each write operation.
	migrationBatchSize int
}

// Create a new MigrationManager. Channeling reads/writes through this migration manager will cause the migration
// to progress. Feeding reads and writes through a migration manager after a migration has completed is equivalent
// to simply operating on the new database directly.
func NewMigrationManager(
	// For reading values out of the old database.
	oldDBReader DBReader,
	// For writing values to the old database.
	oldDBWriter DBWriter,
	// For reading values out of the new database.
	newDBReader DBReader,
	// For writing values to the new database.
	newDBWriter DBWriter,
	// For iterating through key-value pairs to migrate in the old database.
	iterator MigrationIterator,
	// The number of key-value pairs to migrate after each write operation.
	migrationBatchSize int,
) (*MigrationManager, error) {

	boundaryBytes, ok, err := newDBReader(MigrationStore, []byte(FlatKVMigrationBoundaryKey))
	if err != nil {
		return nil, fmt.Errorf("failed to get migration boundary: %w", err)
	}

	var boundary MigrationBoundary
	if ok {
		boundary, err = DeserializeMigrationBoundary(boundaryBytes)
		if err != nil {
			return nil, fmt.Errorf("failed to deserialize migration boundary: %w", err)
		}
	} else {
		boundary = MigrationBoundaryNotStarted
	}
	iterator.SetBoundary(boundary)

	logger.Info("initialized migration manager", "boundary", boundary.String())

	return &MigrationManager{
		oldDBReader:        oldDBReader,
		oldDBWriter:        oldDBWriter,
		newDBReader:        newDBReader,
		newDBWriter:        newDBWriter,
		iterator:           iterator,
		boundary:           boundary,
		migrationBatchSize: migrationBatchSize,
	}, nil
}

// Read a value from the database. If the requested value is migrated, read it from the new database.
// Otherwise, read it from the old database.
func (m *MigrationManager) Read(store string, key []byte) ([]byte, bool, error) {
	if m.boundary.IsMigrated(store, key) {
		return m.newDBReader(store, key)
	}
	return m.oldDBReader(store, key)
}

// Apply a batch of change sets to the database. Depending on the progress of the migration,
// writes are routed to either the new or old database.
//
// This method will also migrate some keys from the old database to the new database. Although this migration operation
// will change the hash of the databases, it will not change the overall state of the databases (i.e. the values
// returned by reads will be the same).
func (m *MigrationManager) ApplyChangeSets(changesets []*proto.NamedChangeSet) error {

	oldDBChangeSet := make([]*proto.NamedChangeSet, 0)
	// Some keys that we migrate might also be written in the change set, so track via a map to allow us to overwrite.
	newDBChangeMap := make(map[string]map[string]*proto.NamedChangeSet)

	// Get a batch of keys to migrate
	valuesToMigrate, newBoundary, err := m.iterator.NextBatch(m.migrationBatchSize)
	if err != nil {
		return fmt.Errorf("failed to get next batch: %w", err)
	}
	m.boundary = newBoundary

	// Delete the keys from the old database and write them to the new database
	for _, value := range valuesToMigrate {
		// Delete the key from the old database
		oldDBChangeSet = append(oldDBChangeSet, &proto.NamedChangeSet{
			Name: value.ModuleName,
			Changeset: proto.ChangeSet{
				Pairs: []*proto.KVPair{
					{Key: value.Key, Delete: true},
				},
			},
		})
		// Write the key to the new database
		storeChanges, ok := newDBChangeMap[value.ModuleName]
		if !ok {
			storeChanges = make(map[string]*proto.NamedChangeSet)
			newDBChangeMap[value.ModuleName] = storeChanges
		}
		storeChanges[string(value.Key)] = &proto.NamedChangeSet{
			Name: value.ModuleName,
			Changeset: proto.ChangeSet{
				Pairs: []*proto.KVPair{
					{Key: value.Key, Value: value.Value},
				},
			},
		}
	}

	// For each pair in the original change sets, route to the appropriate database.
	for _, changeSet := range changesets {
		var oldPairs []*proto.KVPair
		for _, pair := range changeSet.Changeset.Pairs {
			if m.boundary.IsMigrated(changeSet.Name, pair.Key) {
				storeChanges, ok := newDBChangeMap[changeSet.Name]
				if !ok {
					storeChanges = make(map[string]*proto.NamedChangeSet)
					newDBChangeMap[changeSet.Name] = storeChanges
				}
				storeChanges[string(pair.Key)] = &proto.NamedChangeSet{
					Name:      changeSet.Name,
					Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{pair}},
				}
			} else {
				oldPairs = append(oldPairs, pair)
			}
		}
		if len(oldPairs) > 0 {
			oldDBChangeSet = append(oldDBChangeSet, &proto.NamedChangeSet{
				Name:      changeSet.Name,
				Changeset: proto.ChangeSet{Pairs: oldPairs},
			})
		}
	}

	// Flatten writes to new DB and sort for determinism (in case the DB hash is order sensitive).
	storeNames := make([]string, 0, len(newDBChangeMap))
	for name := range newDBChangeMap {
		storeNames = append(storeNames, name)
	}
	sort.Strings(storeNames)

	newDBChangeSets := make([]*proto.NamedChangeSet, 0, len(storeNames))
	for _, name := range storeNames {
		storeChanges := newDBChangeMap[name]
		pairs := make([]*proto.KVPair, 0, len(storeChanges))
		for _, cs := range storeChanges {
			pairs = append(pairs, cs.Changeset.Pairs[0])
		}
		sort.Slice(pairs, func(i, j int) bool {
			return string(pairs[i].Key) < string(pairs[j].Key)
		})
		newDBChangeSets = append(newDBChangeSets, &proto.NamedChangeSet{
			Name:      name,
			Changeset: proto.ChangeSet{Pairs: pairs},
		})
	}

	// Write the new boundary into the new DB so we have the proper boundary if we restart/sync.
	newDBChangeSets = append(newDBChangeSets, &proto.NamedChangeSet{
		Name: MigrationStore,
		Changeset: proto.ChangeSet{
			Pairs: []*proto.KVPair{
				{Key: []byte(FlatKVMigrationBoundaryKey), Value: newBoundary.Serialize()},
			},
		},
	})

	// Apply changes to each database in parallel.
	oldDBErr := make(chan error, 1)
	newDBErr := make(chan error, 1)
	go func() {
		err := m.oldDBWriter(oldDBChangeSet)
		if err != nil {
			err = fmt.Errorf("failed to apply changes to old database: %w", err)
		}
		oldDBErr <- err
	}()
	go func() {
		err := m.newDBWriter(newDBChangeSets)
		if err != nil {
			err = fmt.Errorf("failed to apply changes to new database: %w", err)
		}
		newDBErr <- err
	}()

	err = errors.Join(<-oldDBErr, <-newDBErr)
	if err != nil {
		return fmt.Errorf("failed to apply changes to databases: %w", err)
	}

	return nil
}
