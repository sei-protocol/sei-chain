package wal

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type ErrBadOffset struct{ error }

type logView struct {
	headPath string
	firstIdx int
	nextIdx  int
}

func (v *logView) tailPath(idx int) string {
	return fmt.Sprintf("%s.%03d", v.headPath, idx)
}

func (v *logView) PathByOffset(fileOffset int) (string, error) {
	if wantMin := v.firstIdx - v.nextIdx; fileOffset > 0 || fileOffset < wantMin {
		return "", ErrBadOffset{fmt.Errorf("invalid offset %d, available offsets are [%d,0]", fileOffset, wantMin)}
	}
	if fileOffset == 0 {
		return v.headPath, nil
	}
	return v.tailPath(v.nextIdx + fileOffset), nil
}

func loadLogView(headPath string) (*logView, error) {
	groupDir := filepath.Dir(headPath)
	headBase := filepath.Base(headPath)

	entries, err := os.ReadDir(groupDir)
	if err != nil {
		return nil, err
	}
	minIdx := math.MaxInt
	maxIdx := 0
	// Find the idx range.
	for _, e := range entries {
		suffix, ok := strings.CutPrefix(e.Name(), headBase+".")
		if !ok || suffix == "lock" {
			continue
		}
		idx, err := strconv.Atoi(suffix)
		if err != nil {
			return nil, fmt.Errorf("file %q has invalid suffix %q", e.Name(), suffix)
		}
		minIdx = min(minIdx, idx)
		maxIdx = max(maxIdx, idx)
	}
	v := &logView{headPath: headPath}
	if minIdx <= maxIdx {
		v.firstIdx = minIdx
		v.nextIdx = maxIdx + 1
	}
	return v, nil
}

func (v *logView) TailSize() (int64, error) {
	total := int64(0)
	for idx := v.firstIdx; idx < v.nextIdx; idx++ {
		fi, err := os.Stat(v.tailPath(idx))
		if err != nil {
			return 0, err
		}
		total += fi.Size()
	}
	return total, nil
}

func (v *logView) Rotate(cfg *Config) error {
	// Move head to tail.
	if err := os.Rename(v.headPath, v.tailPath(v.nextIdx)); err != nil {
		return fmt.Errorf("os.Rename(): %w", err)
	}
	v.nextIdx++
	// truncate to acceptable size.
	if cfg.TotalSizeLimit <= 0 {
		return nil
	}
	// There is no head, so just fetch tail size.
	tail, err := v.TailSize()
	if err != nil {
		return fmt.Errorf("v.TailSize(): %w", err)
	}
	for tail > cfg.TotalSizeLimit {
		path := v.tailPath(v.firstIdx)
		fi, err := os.Stat(path)
		if err != nil {
			return err
		}
		if err := os.Remove(path); err != nil {
			return err
		}
		v.firstIdx += 1
		tail -= fi.Size()
	}
	return nil
}
