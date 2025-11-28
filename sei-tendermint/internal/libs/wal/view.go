package wal 

import (
	"math"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type ErrBadOffset struct {error}

func fileSize(path string) (int64,error) {
	fi,err:=os.Stat(path)
	if err!=nil {
		if errors.Is(err,os.ErrNotExist) {
			return 0,nil
		}
		return 0,fmt.Errorf("os.Stat(%q): %w",path,err)
	}
	return fi.Size(),nil
}

type logView struct {
	headPath string
	firstIdx int
	nextIdx int
}

func (v *logView) tailPath(idx int) string {
	return fmt.Sprintf("%s.%03d", v.headPath, idx)
}

func (v *logView) PathByOffset(fileOffset int) (string,error) {
	if wantMin:=v.firstIdx-v.nextIdx; fileOffset>0 || fileOffset<wantMin {
		return "",ErrBadOffset{fmt.Errorf("invalid offset %d, available offsets are [%d,0]",fileOffset,wantMin)}
	}
	if fileOffset==0 {
		return v.headPath,nil
	}
	return v.tailPath(v.nextIdx+fileOffset),nil
}

func loadLogView(headPath string) (*logView,error) {
	groupDir := filepath.Dir(headPath)
	headBase := filepath.Base(headPath)
	
	entries, err := os.ReadDir(groupDir)
	if err != nil {
		return nil,err
	}
	minIdx := math.MaxInt
	maxIdx := 0	
	// Find the idx range.
	for _,e := range entries {
		suffix, ok := strings.CutPrefix(e.Name(), headBase+".")
		if !ok || suffix=="lock" {
			continue
		}
		idx,err := strconv.Atoi(suffix)
		if err!=nil {
			return nil, fmt.Errorf("file %q has invalid suffix %q",e.Name(),suffix)
		}
		minIdx = min(minIdx,idx)
		maxIdx = max(maxIdx,idx)
	}
	v := &logView {headPath:headPath}
	if minIdx <= maxIdx {
		v.firstIdx = minIdx
		v.nextIdx = maxIdx+1
	}
	return v,nil
}

func (v *logView) TailSize() (int64,error) {
	total := int64(0)
	for i:=v.firstIdx; i<v.nextIdx; i++ {
		fs,err := fileSize(v.tailPath(v.firstIdx+i))
		if err!=nil { return 0,err }
		total += fs
	}
	return total,nil
}

func (v *logView) Rotate(cfg *Config) error {
	// Move head to tail.
	if err:=os.Rename(v.headPath,v.tailPath(v.nextIdx)); err!=nil {
		return fmt.Errorf("os.Rename(): %w",err)
	}
	v.nextIdx++
	// truncate tail to acceptable size.
	if cfg.TotalSizeLimit<=0 {
		return nil
	}
	sizes := make([]int64,v.nextIdx-v.firstIdx)
	total := int64(0)
	for i := range sizes {
		size,err := fileSize(v.tailPath(v.firstIdx+i))
		if err!=nil { return err }
		sizes[i] = size
		total += size
	}
	for i:=0; i<len(sizes) && total>cfg.TotalSizeLimit; i++ {
		if err:=os.Remove(v.tailPath(v.firstIdx)); err!=nil {
			return err
		}
		v.firstIdx += 1
		total -= sizes[i]
	}
	return nil
}
