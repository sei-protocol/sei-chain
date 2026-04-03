package segment

import (
	"fmt"
	"math"
	"os"
	"path"
	"time"

	"github.com/Layr-Labs/eigenda/litt/util"
	"github.com/Layr-Labs/eigensdk-go/logging"
)

// scanDirectories scans directories for segment files and returns a map of metadata, key, and value files.
// Also returns a list of garbage files that should be deleted. Does not do anything to files with unrecognized
// extensions.
func scanDirectories(logger logging.Logger, segmentPaths []*SegmentPath) (
	metadataFiles map[uint32]string,
	keyFiles map[uint32]string,
	valueFiles map[uint32][]string,
	garbageFiles []string,
	highestSegmentIndex uint32,
	lowestSegmentIndex uint32,
	err error) {

	highestSegmentIndex = uint32(0)
	lowestSegmentIndex = uint32(math.MaxUint32)

	// key is the file's segment index, value is the file's path
	metadataFiles = make(map[uint32]string)
	keyFiles = make(map[uint32]string)
	valueFiles = make(map[uint32][]string)

	garbageFiles = make([]string, 0)

	for _, segmentPath := range segmentPaths {
		files, err := os.ReadDir(segmentPath.SegmentDirectory())
		if err != nil {
			return nil, nil, nil, nil, 0, 0,
				fmt.Errorf("failed to read directory %s: %v", segmentPath.SegmentDirectory(), err)
		}

		for _, file := range files {
			if file.IsDir() {
				continue
			}

			fileName := file.Name()
			extension := path.Ext(fileName)
			filePath := path.Join(segmentPath.SegmentDirectory(), fileName)
			var index uint32

			switch extension {
			case MetadataSwapExtension, KeyFileSwapExtension:
				garbageFiles = append(garbageFiles, filePath)
				continue
			case MetadataFileExtension:
				index, err = getMetadataFileIndex(fileName)
				if err != nil {
					return nil, nil, nil, nil,
						0, 0,
						fmt.Errorf("failed to get file index: %v", err)
				}
				metadataFiles[index] = filePath
			case KeyFileExtension:
				index, err = getKeyFileIndex(fileName)
				if err != nil {
					return nil, nil, nil, nil,
						0, 0, fmt.Errorf("failed to get file index: %v", err)
				}
				keyFiles[index] = filePath
			case ValuesFileExtension:
				index, err = getValueFileIndex(fileName)
				if err != nil {
					return nil, nil, nil, nil,
						0, 0, fmt.Errorf("failed to get file index: %v", err)
				}
				valueFiles[index] = append(valueFiles[index], filePath)
			default:
				logger.Debugf("Ignoring unknown file %s", filePath)
				continue
			}

			if index > highestSegmentIndex {
				highestSegmentIndex = index
			}
			if index < lowestSegmentIndex {
				lowestSegmentIndex = index
			}
		}
	}

	if lowestSegmentIndex == math.MaxUint32 {
		// No segments found, fix the index.
		lowestSegmentIndex = 0
	}

	return metadataFiles,
		keyFiles,
		valueFiles,
		garbageFiles,
		highestSegmentIndex,
		lowestSegmentIndex,
		nil
}

// diagnoseMissingFile decides what to do with specific missing files. If the segment is either the segment
// with the lowest index or the segment with the highest index, it is possible for files to be missing due to
// non-catastrophic reasons (i.e. a crash during cleanup). If the segment is neither the lowest nor highest segment,
// then missing files signal non-recoverable DB corruption, and an error is returned.
func diagnoseMissingFile(
	logger logging.Logger,
	index uint32,
	lowestFileIndex uint32,
	highestFileIndex uint32,
	fileType string,
	damagedSegments map[uint32]struct{}) error {

	if index == highestFileIndex {
		// This can happen if we crash while creating a new segment. Recoverable.
		logger.Warnf("Missing %s file for last segment %d", fileType, index)
		damagedSegments[index] = struct{}{}
	} else if index == lowestFileIndex {
		// This can happen when deleting the oldest segment. Recoverable.
		logger.Warnf("Missing %s file for first segment %d", fileType, index)
		damagedSegments[index] = struct{}{}
	} else {
		// Database is missing internal files. Catastrophic failure.
		return fmt.Errorf("missing %s file for segment %d", fileType, index)
	}

	return nil
}

// lookForMissingFiles ensures that all files that should be present are actually present. Returns an error
// if files are missing in a way that cannot be recovered. If recoverable, returns a list of orphaned files.
// An "orphaned file" is defined as a file on disk for a segment that is missing one or more of its files.
// For example, if a segment has a metadata file but is missing its key file, the metadata file is considered orphaned.
func lookForMissingFiles(
	logger logging.Logger,
	lowestSegmentIndex uint32,
	highestSegmentIndex uint32,
	metadataFiles map[uint32]string,
	keyFiles map[uint32]string,
	valueFiles map[uint32][]string,
	fsync bool,
) (orphanedFiles []string, damagedSegments map[uint32]struct{}, error error) {

	orphanedFiles = make([]string, 0)
	damagedSegments = make(map[uint32]struct{})

	for segment := lowestSegmentIndex; segment <= highestSegmentIndex; segment++ {

		if segment == 0 && len(metadataFiles) == 0 && len(keyFiles) == 0 && len(valueFiles) == 0 {
			// Special case, only happens when starting a table from scratch.
			// Files aren't actually missing, so no need to log anything.
			break
		}

		potentialOrphans := make([]string, 0)
		segmentMissingFiles := false

		// Check for missing metadata file.
		_, metadataPresent := metadataFiles[segment]
		if metadataPresent {
			potentialOrphans = append(potentialOrphans, metadataFiles[segment])
		} else {
			segmentMissingFiles = true
			err := diagnoseMissingFile(
				logger,
				segment,
				lowestSegmentIndex,
				highestSegmentIndex,
				"metadata",
				damagedSegments)
			if err != nil {
				return nil, nil, err
			}
		}

		// Check for missing key file.
		_, keysPresent := keyFiles[segment]
		if keysPresent {
			potentialOrphans = append(potentialOrphans, keyFiles[segment])
		} else {
			segmentMissingFiles = true
			err := diagnoseMissingFile(
				logger,
				segment,
				lowestSegmentIndex,
				highestSegmentIndex,
				"key",
				damagedSegments)
			if err != nil {
				return nil, nil, err
			}
		}

		// Check for missing value files (there should be exactly one value file per shard).
		if !metadataPresent {
			// If the metadata file is missing but we haven't yet returned an error, all of the value files
			// are automatically considered orphaned.
			orphanedFiles = append(orphanedFiles, valueFiles[segment]...)
		} else {

			// We need to know the sharding factor to check for missing value files.
			metadataPath := metadataFiles[segment]
			metadataDirectory := path.Dir(metadataPath)

			metadata, err := loadMetadataFile(segment, []*SegmentPath{{segmentDirectory: metadataDirectory}}, fsync)
			if err != nil {
				return nil, nil,
					fmt.Errorf("failed to load metadata file: %v", err)
			}

			if uint32(len(valueFiles[segment])) > metadata.shardingFactor {
				return nil, nil,
					fmt.Errorf("too many value files for segment %d, expected at most %d, got %d",
						segment, metadata.shardingFactor, len(valueFiles[segment]))
			}

			// Catalogue the shards we do have.
			shardsPresent := make(map[uint32]struct{})
			for _, vFile := range valueFiles[segment] {
				shard, err := getValueFileShard(vFile)
				if err != nil {
					return nil, nil,
						fmt.Errorf("failed to get shard from value file: %v", err)
				}
				shardsPresent[shard] = struct{}{}
				potentialOrphans = append(potentialOrphans, vFile)
			}

			// Check that we have each shard.
			for shard := uint32(0); shard < metadata.shardingFactor; shard++ {
				_, shardPresent := shardsPresent[shard]
				if !shardPresent {
					segmentMissingFiles = true
					err = diagnoseMissingFile(
						logger,
						segment,
						lowestSegmentIndex,
						highestSegmentIndex,
						fmt.Sprintf("shard-%d", shard),
						damagedSegments)
					if err != nil {
						return nil, nil, err
					}
				}
			}
		}

		if segmentMissingFiles {
			// If we are missing a file in this segment, all other files in the segment are considered orphaned.
			orphanedFiles = append(orphanedFiles, potentialOrphans...)
		}
	}

	return orphanedFiles, damagedSegments, nil
}

// deleteOrphanedFiles deletes any files that are in the orphan set.
func deleteOrphanedFiles(logger logging.Logger, orphanedFiles []string) error {
	for _, orphanedFile := range orphanedFiles {
		logger.Infof("deleting orphaned file %s", orphanedFile)
		err := os.Remove(orphanedFile)
		if err != nil {
			return fmt.Errorf("failed to remove orphaned file %s: %v", orphanedFile, err)
		}
	}
	return nil
}

// linkSegments links together adjacent segments via SetNextSegment().
func linkSegments(lowestSegmentIndex uint32, highestSegmentIndex uint32, segments map[uint32]*Segment) error {
	if lowestSegmentIndex == highestSegmentIndex {
		// Only one segment, nothing to link. This is checked explicitly to avoid 0-1 underflow.
		return nil
	}

	for i := lowestSegmentIndex; i < highestSegmentIndex; i++ {
		first, ok := segments[i]
		if !ok {
			return fmt.Errorf("missing segment %d", i)
		}
		second, ok := segments[i+1]
		if !ok {
			return fmt.Errorf("missing segment %d", i+1)
		}
		first.SetNextSegment(second)
	}
	return nil
}

// GatherSegmentFiles scans a directory for segment files and loads them into memory.
func GatherSegmentFiles(
	logger logging.Logger,
	errorMonitor *util.ErrorMonitor,
	segmentPaths []*SegmentPath,
	snapshottingEnabled bool,
	now time.Time,
	cleanOrphans bool,
	fsync bool,
) (lowestSegmentIndex uint32, highestSegmentIndex uint32, segments map[uint32]*Segment, err error) {

	// Scan the root directories for segment files.
	metadataFiles, keyFiles, valueFiles, garbageFiles, highestSegmentIndex, lowestSegmentIndex, err :=
		scanDirectories(logger, segmentPaths)
	if err != nil {
		return 0, 0, nil,
			fmt.Errorf("failed to scan directory: %v", err)
	}

	segments = make(map[uint32]*Segment)

	// Delete any garbage files. Ignore files with unrecognized extensions.
	for _, garbageFile := range garbageFiles {
		logger.Infof("deleting file %s", garbageFile)
		err = os.Remove(garbageFile)
		if err != nil {
			return 0, 0, nil,
				fmt.Errorf("failed to remove garbage file %s: %v", garbageFile, err)
		}
	}

	// Check for missing files.
	orphanedFiles, damagedSegments, err := lookForMissingFiles(
		logger,
		lowestSegmentIndex,
		highestSegmentIndex,
		metadataFiles,
		keyFiles,
		valueFiles,
		fsync)
	if err != nil {
		return 0, 0, nil,
			fmt.Errorf("there are one or more missing files: %v", err)
	}

	if cleanOrphans {
		// Clean up any orphaned segment files.
		err = deleteOrphanedFiles(logger, orphanedFiles)
		if err != nil {
			return 0, 0, nil,
				fmt.Errorf("failed to delete orphaned files: %v", err)
		}
	}

	if len(metadataFiles) > 0 {
		// Adjust the segment range to exclude orphaned segments.
		if _, ok := damagedSegments[highestSegmentIndex]; ok {
			highestSegmentIndex--
		}
		if _, ok := damagedSegments[lowestSegmentIndex]; ok {
			lowestSegmentIndex++
		}

		// Load all healthy segments.
		for i := lowestSegmentIndex; i <= highestSegmentIndex; i++ {
			segment, err := LoadSegment(logger, errorMonitor, i, segmentPaths, snapshottingEnabled, now, fsync)
			if err != nil {
				return 0, 0, nil,
					fmt.Errorf("failed to create segment %d: %v", i, err)
			}
			segments[i] = segment
		}

		// Stitch together the segments.
		err = linkSegments(lowestSegmentIndex, highestSegmentIndex, segments)
		if err != nil {
			return 0, 0, nil,
				fmt.Errorf("failed to link segments: %v", err)
		}
	}

	return lowestSegmentIndex, highestSegmentIndex, segments, nil
}
