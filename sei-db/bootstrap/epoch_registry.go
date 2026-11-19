package bootstrap

// EpochStateModule is the FlatKV module the epoch registry is stored under.
const EpochStateModule = "autobahn"

// EpochRegistryKey is the FlatKV key of the persisted epoch registry.
const EpochRegistryKey = "epoch_registry"

// EpochRegistry returns the persisted epoch registry and whether a value is stored.
func (m *GigaStorageManager) EpochRegistry() ([]byte, bool) {
	return m.stateDB.SC().Get(EpochStateModule, []byte(EpochRegistryKey))
}
