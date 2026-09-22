package receipt

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// receiptBackendMarkerFileName is the file in a receipt store's directory naming the backend that owns it.
const receiptBackendMarkerFileName = "receipt-backend.txt"

// receiptBackendMarkerVersion is the marker format version this build writes.
const receiptBackendMarkerVersion = 1

// requireSupportedBackend fails if backend is not a receipt store backend this build implements. backend
// must already be normalized.
func requireSupportedBackend(backend string) error {
	switch backend {
	case receiptBackendLittIdx, receiptBackendPebble:
		return nil
	default:
		return fmt.Errorf("unsupported receipt store backend: %s", backend)
	}
}

// recordBackendType refuses dbDirectory if a different backend already owns it, and otherwise records
// backend as its owner, creating the directory if it does not exist. A directory carrying no marker is
// adopted rather than refused: it was either written before markers existed or is new, so backend is
// taken to be its owner. backend must already be normalized.
func recordBackendType(dbDirectory string, backend string) error {
	if err := os.MkdirAll(dbDirectory, 0o750); err != nil {
		return fmt.Errorf("failed to create receipt store directory %s: %w", dbDirectory, err)
	}

	owner, found, err := readBackendType(dbDirectory)
	if err != nil {
		return err
	}
	if found {
		if owner != backend {
			return backendMismatchError(dbDirectory, owner, backend)
		}
		return nil
	}
	return writeBackendType(dbDirectory, backend)
}

// requireBackendType fails if dbDirectory is marked as belonging to a backend other than want. A
// directory carrying no marker is not a failure: it was written before markers existed, so want stands.
// It does not write, so a caller pointed at the wrong directory does not record a type there.
func requireBackendType(dbDirectory string, want string) error {
	owner, found, err := readBackendType(dbDirectory)
	if err != nil {
		return err
	}
	if found && owner != want {
		return backendMismatchError(dbDirectory, owner, want)
	}
	return nil
}

// readBackendType returns the backend recorded as the owner of dbDirectory. found is false when the
// directory carries no marker. A marker that cannot be parsed, or that names a format version or a backend
// this build does not know, is an error: the directory holds something this build cannot reason about, and
// treating that as an absent marker would defeat the check.
func readBackendType(dbDirectory string) (backend string, found bool, err error) {
	filePath := filepath.Join(dbDirectory, receiptBackendMarkerFileName)

	contents, err := os.ReadFile(filePath) //nolint:gosec // path within the configured store directory
	if os.IsNotExist(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("failed to read receipt backend marker %s: %w", filePath, err)
	}

	lines := strings.Split(strings.TrimSpace(string(contents)), "\n")
	if len(lines) != 2 {
		return "", false, fmt.Errorf("receipt backend marker %s is malformed: want 2 lines, got %d",
			filePath, len(lines))
	}

	version, err := strconv.Atoi(strings.TrimSpace(lines[0]))
	if err != nil {
		return "", false, fmt.Errorf("receipt backend marker %s has an unparseable version %q: %w",
			filePath, lines[0], err)
	}
	if version != receiptBackendMarkerVersion {
		return "", false, fmt.Errorf("receipt backend marker %s has unsupported version %d (want %d)",
			filePath, version, receiptBackendMarkerVersion)
	}

	backend = strings.TrimSpace(lines[1])
	if err := requireSupportedBackend(backend); err != nil {
		return "", false, fmt.Errorf("receipt backend marker %s: %w", filePath, err)
	}
	return backend, true, nil
}

// writeBackendType records backend as the owner of dbDirectory, which must already exist.
func writeBackendType(dbDirectory string, backend string) error {
	filePath := filepath.Join(dbDirectory, receiptBackendMarkerFileName)
	contents := fmt.Sprintf("%d\n%s\n", receiptBackendMarkerVersion, backend)
	if err := os.WriteFile(filePath, []byte(contents), 0o600); err != nil {
		return fmt.Errorf("failed to write receipt backend marker %s: %w", filePath, err)
	}
	return nil
}

// backendMismatchError describes a store directory that one backend owns being opened as another.
func backendMismatchError(dbDirectory string, owner string, want string) error {
	return fmt.Errorf("receipt store at %s belongs to the %q backend, but the %q backend was requested; "+
		"point at that backend's store directory or configure the backend that owns this one",
		dbDirectory, owner, want)
}
