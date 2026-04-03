package segment

import (
	"fmt"
	"os"
	"path"
	"path/filepath"

	"github.com/Layr-Labs/eigenda/litt/util"
)

// The name of the directory where segment files are stored. The segment directory is created at
// "$STORAGE_PATH/$TABLE_NAME/segments". Each table has at least one segment directory. Tables may
// have multiple segment directories if more than one path is provided to Litt.Config.Paths.
const SegmentDirectory = "segments"

// The name of the directory where hard links to segment files are stored for snapshotting (if enabled).
// The hard link directory is created at "$STORAGE_PATH/$TABLE_NAME/snapshot".
const HardLinkDirectory = "snapshot"

// SegmentPath encapsulates various file paths utilized by segment files.
type SegmentPath struct {
	// The directory where the segment file is stored.
	segmentDirectory string
	// If snapshotting is enabled, the directory where a Snapshot will put a hard link to the segment file.
	// An empty string if snapshotting is not enabled.
	hardlinkPath string
	// If snapshotting is enabled, the directory where a Snapshot will put a soft link to the hard link of a
	// segment file. An empty string if snapshotting is not enabled.
	softlinkPath string
}

// NewSegmentPath creates a new SegmentPath. Each segment file's location on disk is determined by a SegmentPath object.
//
// The storageRoot is a location where LittDB is storing data, i.e. one of the paths from Litt.Config.Paths.
//
// softlinkRoot will be an empty string if snapshotting is not enabled, or a path to the root directory where
// Snapshot soft links will be created. The presence (or absence) of this path is used by LittDB to
// determine if snapshotting is enabled.
//
// The tableName is the name of the table that owns the segment file.
func NewSegmentPath(
	storageRoot string,
	softlinkRoot string,
	tableName string,
) (*SegmentPath, error) {

	if storageRoot == "" {
		return nil, fmt.Errorf("storage path cannot be empty")
	}

	segmentDirectory := path.Join(storageRoot, tableName, SegmentDirectory)

	softlinkPath := ""
	hardLinkPath := ""
	if softlinkRoot != "" {
		softlinkPath = path.Join(softlinkRoot, tableName, SegmentDirectory)
		hardLinkPath = path.Join(storageRoot, tableName, HardLinkDirectory)
	}

	return &SegmentPath{
		segmentDirectory: segmentDirectory,
		hardlinkPath:     hardLinkPath,
		softlinkPath:     softlinkPath,
	}, nil
}

// BuildSegmentPaths creates a list of SegmentPath objects for each storage root provided.
func BuildSegmentPaths(
	storageRoots []string,
	softlinkRoot string,
	tableName string,
) ([]*SegmentPath, error) {
	segmentPaths := make([]*SegmentPath, len(storageRoots))
	for i, storageRoot := range storageRoots {
		segmentPath, err := NewSegmentPath(storageRoot, softlinkRoot, tableName)
		if err != nil {
			return nil, fmt.Errorf("error building segment path: %v", err)
		}
		segmentPaths[i] = segmentPath
	}
	return segmentPaths, nil
}

// SegmentDirectory returns the parent directory where segment files are stored.
func (p *SegmentPath) SegmentDirectory() string {
	return p.segmentDirectory
}

// HardlinkPath returns the path where hard links to segment files will be created for snapshotting.
func (p *SegmentPath) HardlinkPath() string {
	return p.hardlinkPath
}

// SoftlinkPath returns the path where soft links to hard links of segment files will be created for snapshotting.
func (p *SegmentPath) SoftlinkPath() string {
	return p.softlinkPath
}

// snapshottingEnabled checks if snapshotting is enabled.
func (p *SegmentPath) snapshottingEnabled() bool {
	return p.softlinkPath != ""
}

// MakeDirectories creates the necessary directories described by the SegmentPath if they do not already exist.
func (p *SegmentPath) MakeDirectories(fsync bool) error {
	err := util.EnsureDirectoryExists(p.segmentDirectory, fsync)
	if err != nil {
		return fmt.Errorf("failed to ensure segment directory exists: %w", err)
	}

	if p.snapshottingEnabled() {
		err = util.EnsureDirectoryExists(p.hardlinkPath, fsync)
		if err != nil {
			return fmt.Errorf("failed to ensure hard link directory exists: %w", err)
		}

		err = util.EnsureDirectoryExists(p.softlinkPath, fsync)
		if err != nil {
			return fmt.Errorf("failed to ensure soft link directory exists: %w", err)
		}
	}

	return nil
}

// Snapshot creates a hard link to the file in the Snapshot directory, and a symlink to that hard link in the soft link
// directory. The fileName should just be the name of the file, not its full path. The file is expected to be in the
// segmentDirectory.
func (p *SegmentPath) Snapshot(fileName string) error {
	if !p.snapshottingEnabled() {
		return fmt.Errorf("snapshotting is not enabled, cannot Snapshot file %s", fileName)
	}

	sourcePath := filepath.Join(p.segmentDirectory, fileName)
	hardlinkPath := filepath.Join(p.hardlinkPath, fileName)
	symlinkPath := filepath.Join(p.softlinkPath, fileName)

	err := os.Link(sourcePath, hardlinkPath)
	if err != nil && !os.IsExist(err) {
		return fmt.Errorf("failed to create hard link from %s to %s: %v", sourcePath, hardlinkPath, err)
	}

	err = os.Symlink(hardlinkPath, symlinkPath)
	if err != nil {
		return fmt.Errorf("failed to create symlink from %s to %s: %v", hardlinkPath, symlinkPath, err)
	}

	return nil
}
