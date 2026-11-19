package statesync

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"
	"time"
)

const (
	keyDir = "giga-state-sync"
	// maxLatestSize is the largest latest.txt object that ReadLatest reads.
	maxLatestSize = 64
	// maxAdvanceAttempts is the number of conditional writes that AdvanceLatest tries.
	maxAdvanceAttempts = 5
	// maxClaimSize is the largest upload claim object that ClaimUpload reads.
	maxClaimSize = 256
)

// ErrObjectNotFound is the error that an ObjectStore returns when no object has the key.
var ErrObjectNotFound = errors.New("object not found")

// ErrPreconditionFailed is the error that an ObjectStore returns when the object at the key of a
// Put does not meet the Precondition of the Put.
var ErrPreconditionFailed = errors.New("precondition failed")

// Precondition is what a Put requires of the object at its key. The zero value requires nothing.
type Precondition struct {
	// Absent requires that no object has the key.
	Absent bool
	// Version, when not empty, requires that the object at the key has this version.
	Version string
}

// ObjectInfo is the metadata of the content of an object.
type ObjectInfo struct {
	// Version identifies the content, for Precondition.Version.
	Version string
	// Modified is the time that the store wrote the content.
	Modified time.Time
}

// ObjectStore stores objects by key.
type ObjectStore interface {
	// Put stores the size bytes that body yields as the object at key. It returns an error wrapping
	// ErrPreconditionFailed when the object at key does not meet pre, and stores nothing then.
	Put(ctx context.Context, key string, body io.Reader, size int64, pre Precondition) error
	// Get returns the content of the object at key and its metadata. It returns an error wrapping
	// ErrObjectNotFound when there is no object at key.
	Get(ctx context.Context, key string) (io.ReadCloser, ObjectInfo, error)
}

// ArchiveKey returns the key of the archive of chain chainID at height. The height has 20 digits,
// so the keys of a chain sort by height.
func ArchiveKey(chainID string, height int64) string {
	return path.Join(keyDir, chainID, fmt.Sprintf("%020d.tar", height))
}

// LatestKey returns the key of the object that holds the newest archive height of chain chainID.
func LatestKey(chainID string) string {
	return path.Join(keyDir, chainID, "latest.txt")
}

// ClaimKey returns the key of the upload claim of chain chainID.
func ClaimKey(chainID string) string {
	return path.Join(keyDir, chainID, "upload-claim")
}

// ReadLatest returns the newest archive height of chain chainID in store. It returns an error
// wrapping ErrObjectNotFound when store has no archive height for the chain.
func ReadLatest(ctx context.Context, store ObjectStore, chainID string) (int64, error) {
	height, _, err := ReadLatestInfo(ctx, store, chainID)
	return height, err
}

// ReadLatestInfo returns the newest archive height of chain chainID in store, and the metadata of
// the object that holds it. It returns an error wrapping ErrObjectNotFound when store has no
// archive height for the chain.
func ReadLatestInfo(ctx context.Context, store ObjectStore, chainID string) (int64, ObjectInfo, error) {
	key := LatestKey(chainID)
	data, info, err := readSmall(ctx, store, key, maxLatestSize)
	if err != nil {
		return 0, ObjectInfo{}, err
	}
	height, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return 0, ObjectInfo{}, fmt.Errorf("%s: %w", key, err)
	}
	if height < 1 {
		return 0, ObjectInfo{}, fmt.Errorf("%s: height %d is not positive", key, height)
	}
	return height, info, nil
}

// readSmall returns the content of the object at key, which has at most limit bytes, and its
// metadata.
func readSmall(ctx context.Context, store ObjectStore, key string, limit int64) ([]byte, ObjectInfo, error) {
	body, info, err := store.Get(ctx, key)
	if err != nil {
		return nil, ObjectInfo{}, err
	}
	defer func() { _ = body.Close() }()
	data, err := io.ReadAll(io.LimitReader(body, limit+1))
	if err != nil {
		return nil, ObjectInfo{}, fmt.Errorf("read %s: %w", key, err)
	}
	if int64(len(data)) > limit {
		return nil, ObjectInfo{}, fmt.Errorf("%s is more than %d bytes", key, limit)
	}
	return data, info, nil
}

// AdvanceLatest records height as the newest archive height of chain chainID in store, unless
// store records that height or a higher one. It returns the newest archive height that store
// records after the call.
func AdvanceLatest(ctx context.Context, store ObjectStore, chainID string, height int64) (int64, error) {
	key := LatestKey(chainID)
	data := []byte(strconv.FormatInt(height, 10) + "\n")
	for range maxAdvanceAttempts {
		latest, info, err := ReadLatestInfo(ctx, store, chainID)
		pre := Precondition{Version: info.Version}
		switch {
		case errors.Is(err, ErrObjectNotFound):
			pre = Precondition{Absent: true}
		case err != nil:
			return 0, err
		case latest >= height:
			return latest, nil
		}
		err = store.Put(ctx, key, bytes.NewReader(data), int64(len(data)), pre)
		if errors.Is(err, ErrPreconditionFailed) {
			continue
		}
		if err != nil {
			return 0, err
		}
		return height, nil
	}
	return 0, fmt.Errorf("%s changed during each of %d writes", key, maxAdvanceAttempts)
}

// ClaimUpload makes owner the node that uploads the next archive of chain chainID, the first above
// height above, the newest height in latest.txt or 0. It returns false when the claim belongs to
// another node: for the same round, and written less than timeout before now, or for a later round.
func ClaimUpload(ctx context.Context, store ObjectStore, chainID string, above int64, owner string, now time.Time, timeout time.Duration) (bool, error) {
	key := ClaimKey(chainID)
	data, info, err := readSmall(ctx, store, key, maxClaimSize)
	var pre Precondition
	switch {
	case errors.Is(err, ErrObjectNotFound):
		pre = Precondition{Absent: true}
	case err != nil:
		return false, err
	default:
		claimAbove, claimOwner, ok := parseClaim(data)
		switch {
		case ok && claimAbove > above:
			return false, nil
		case ok && claimAbove == above && claimOwner == owner:
			return true, nil
		case ok && claimAbove == above && now.Sub(info.Modified) < timeout:
			return false, nil
		}
		pre = Precondition{Version: info.Version}
	}
	claim := []byte(fmt.Sprintf("%d %s\n", above, owner))
	err = store.Put(ctx, key, bytes.NewReader(claim), int64(len(claim)), pre)
	if errors.Is(err, ErrPreconditionFailed) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// parseClaim returns the round and the owner that an upload claim records, and false when data is
// not a claim.
func parseClaim(data []byte) (int64, string, bool) {
	fields := strings.Fields(string(data))
	if len(fields) != 2 {
		return 0, "", false
	}
	above, err := strconv.ParseInt(fields[0], 10, 64)
	if err != nil || above < 0 {
		return 0, "", false
	}
	return above, fields[1], true
}
