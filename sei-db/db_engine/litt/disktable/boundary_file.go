package disktable

import (
	"fmt"
	"os"
	"path"
	"strconv"
	"strings"

	"github.com/Layr-Labs/eigenda/litt/util"
)

// The name of the file that defines the lower bound of a LittDB snapshot directory.
const LowerBoundFileName = "lower-bound.txt"

// The name of the file that defines the upper bound of a LittDB snapshot directory.
const UpperBoundFileName = "upper-bound.txt"

// BoundaryType is an enum that describes the type of boundary file.
type BoundaryType bool

const (
	// A boundary file that defines the lowest valid segment index in a snapshot directory.
	LowerBound BoundaryType = true
	// A boundary file that defines the highest valid segment index in a snapshot directory.
	UpperBound BoundaryType = false
)

type BoundaryFile struct {
	// The type of this boundary file.
	boundaryType BoundaryType

	// The parent directory where this file is stored.
	parentDirectory string

	// If true, then the boundary is defined, otherwise it is undefined.
	// If undefined, the boundary index should be considered invalid.
	defined bool

	// The segment index of the boundary. Describes a lower/upper segment index. If this is a lower bound file,
	// it describes the lowest segment index that is valid within the snapshot directory (inclusive). If this is
	// an upper bound file, it describes the highest segment index that is valid within the snapshot directory
	// (also inclusive).
	boundaryIndex uint32
}

// LoadBoundaryFile loads a boundary file from the specified parent directory. If the boundary file does not exist,
// then this method returns an object that can be used to create a new boundary file at the specified path (i.e. by
// calling Write() or Update()).
func LoadBoundaryFile(boundaryType BoundaryType, parentDirectory string) (*BoundaryFile, error) {
	boundary := &BoundaryFile{
		boundaryType:    boundaryType,
		parentDirectory: parentDirectory,
	}

	exists, err := util.Exists(boundary.Path())
	if err != nil {
		return nil, fmt.Errorf("failed to check if boundary file %s exists: %v", boundary.Path(), err)
	}

	if exists {
		data, err := os.ReadFile(boundary.Path())
		if err != nil {
			return nil, fmt.Errorf("failed to read boundary file %s: %v", boundary.Path(), err)
		}

		data = []byte(strings.TrimSpace(string(data)))

		err = boundary.deserialize(data)
		if err != nil {
			return nil, fmt.Errorf("failed to deserialize boundary file %s: %v", boundary.Path(), err)
		}
		boundary.defined = true
	}

	return boundary, nil
}

// Atomically update the value of the boundary file.
func (b *BoundaryFile) Update(newBoundary uint32) error {
	if b == nil {
		return nil
	}

	if newBoundary < b.boundaryIndex {
		return fmt.Errorf("boundary index may only increase, cannot set to %d (current: %d)",
			newBoundary, b.boundaryIndex)
	}

	b.defined = true
	b.boundaryIndex = newBoundary
	err := b.Write()
	if err != nil {
		return fmt.Errorf("failed to update boundary file %s: %v", b.Path(), err)
	}
	return nil
}

// Get the file name of the boundary file.
func (b *BoundaryFile) Name() string {
	if b == nil {
		return ""
	}

	if b.boundaryType == LowerBound {
		return LowerBoundFileName
	}
	return UpperBoundFileName
}

// Get the full path where the boundary file is stored.
func (b *BoundaryFile) Path() string {
	if b == nil {
		return ""
	}

	return path.Join(b.parentDirectory, b.Name())
}

// Serialize the boundary file to a byte slice.
func (b *BoundaryFile) serialize() []byte {
	if b == nil {
		return nil
	}

	// Serialize the boundary file to a byte slice. Since end users may interact with this file,
	// serialize in a human-readable format.
	return []byte(fmt.Sprintf("%d\n", b.boundaryIndex))
}

func (b *BoundaryFile) deserialize(data []byte) error {
	if b == nil {
		return nil
	}

	boundaryIndex, err := strconv.Atoi(string(data))
	if err != nil {
		return fmt.Errorf("failed to parse boundary index from data: %v", err)
	}
	b.boundaryIndex = uint32(boundaryIndex)
	return nil
}

// Write the boundary file to disk.
func (b *BoundaryFile) Write() error {
	if b == nil {
		return nil
	}

	data := b.serialize()
	// fsync is not necessary, in an advent of a crash the boundary files get repaired
	err := util.AtomicWrite(b.Path(), data, false)
	if err != nil {
		return fmt.Errorf("failed to write boundary file %s: %v", b.Path(), err)
	}

	return nil
}

// Returns true if this boundary file is defined. If undefined, it means that the boundary index is invalid
// and should not be used.
func (b *BoundaryFile) IsDefined() bool {
	if b == nil {
		return false
	}

	return b.defined
}

// Get the boundary index described by this file.
//
// If this is a lower bound, then it describes the highest segment index in a snapshot directory that has been garbage
// collected. As a result, LittDB will not snapshot any segments with this index or lower.
//
// If this is an upper bound, then it describes the highest segment index that LittDB has fully taken a snapshot of.
// External processes using the snapshot should ignore any segment with an index greater than this.
func (b *BoundaryFile) BoundaryIndex() uint32 {
	if b == nil {
		return 0
	}

	return b.boundaryIndex
}
