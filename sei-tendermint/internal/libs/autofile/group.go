/*
You can open a Group to keep restrictions on an AutoFile, like
the maximum size of each chunk, and/or the total amount of bytes
stored in the group.

The first file to be written in the Group.Dir is the head file.

	Dir/
	- <HeadPath>

Once the Head file reaches the size limit, it will be rotated.

	Dir/
	- <HeadPath>.000   // First rolled file
	- <HeadPath>       // New head path, starts empty.
										 // The implicit index is 001.

As more files are written, the index numbers grow...

	Dir/
	- <HeadPath>.000   // First rolled file
	- <HeadPath>.001   // Second rolled file
	- ...
	- <HeadPath>       // New head path

The Group can also be used to binary-search for some line,
assuming that marker lines are written occasionally.
*/
package autofile

import (
	"math"
	"slices"
	"context"
	"fmt"
	"io"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"golang.org/x/sys/unix"

	"github.com/tendermint/tendermint/libs/utils"
)

const filePerms = os.FileMode(0600)

type ErrInvalidOffset struct {error}

type Config struct {
	FileSizeLimit      int64
	TotalSizeLimit     int64
}

func DefaultConfig() *Config {
	return &Config {
		FileSizeLimit:       10 * 1024 * 1024,       // 10MB
		TotalSizeLimit:      1 * 1024 * 1024 * 1024, // 1GB
	}
}

type cmd struct {
	Sync utils.Option[chan struct{}]
	Data utils.Option[[]byte]
}

type LogWriter struct {
	headPath string
	config *Config
	cmds chan cmd
}

// OpenGroup creates a new Group with head at headPath. It returns an error if
// it fails to open head file.
func NewLogWriter(headPath string, config *Config) (*LogWriter) {	
	return &LogWriter{headPath,config,make(chan cmd)}
}

func (w *LogWriter) runHeadWriter(ctx context.Context) error {
	fw,err := newFileWriter(w.headPath)
	if err!=nil {
		return err	
	}
	defer fw.Close()
	if err:=func() error {
		for {
			if limit:=w.config.FileSizeLimit; limit>0 && limit>=fw.bytesSize {
				return nil
			}
			cmd,err := utils.Recv(ctx,w.cmds)
			if err!=nil { return err }
			if data,ok := cmd.Data.Get(); ok {
				if err:=fw.Write(data); err!=nil {
					return fmt.Errorf("fw.Write(): %w",err)
				}
			}
			if sync,ok := cmd.Sync.Get(); ok {
				if err:=fw.Sync(); err!=nil {
					return fmt.Errorf("fw.Sync(): %w",err)
				}
				close(sync)
			}
		}
	}(); err!=nil {
		return err
	}
	return fw.Sync()
}

// OnStart implements service.Service by starting the goroutine that checks file
// and group limits.
func (w *LogWriter) Run(ctx context.Context) error {
	g, err := newLogGuard(w.headPath)
	if err != nil {
		return err
	}
	defer g.Close()
	for {
		if err:=w.runHeadWriter(ctx);err!=nil {
			return err
		}
		if err:=g.Rotate(w.config); err!=nil {
			return fmt.Errorf("g.Rotate(): %w",err)
		}	
	}
}

// Write writes entry to the log atomically. You need to call Sync afterwards
// to ensure that the write is persisted.
func (g *LogWriter) Write(ctx context.Context, entry []byte) error {
	return utils.Send(ctx,g.cmds,cmd{Data:utils.Some(slices.Clone(entry))})
}

// Sync writes any buffered data to the underlying file and commits the
// current content of the file to stable storage (fsync).
func (w *LogWriter) Sync(ctx context.Context) error {
	done := make(chan struct{})
	if err:=utils.Send(ctx,w.cmds,cmd{Sync:utils.Some(done)}); err!=nil { return err }
	_,_,err :=utils.RecvOrClosed(ctx,done)
	return err
}

func lockPath(headPath string) string {
	return fmt.Sprintf("%s.lock",headPath)
}

func tailPath(headPath string, idx int) string {
	return fmt.Sprintf("%s.%03d", headPath, idx)
}

type logGuard struct {
	lock    *os.File
	headPath string
	firstIdx int
	nextIdx  int
}

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

// Index includes the head.
func newLogGuard(headPath string) (res *logGuard, resErr error) {
	// Take a lock on the log.
	lock,err := os.OpenFile(lockPath(headPath),os.O_CREATE|os.O_RDONLY,filePerms)
	if err!=nil {
		return nil,err
	}
	defer func(){ if resErr!=nil { lock.Close() } }()
	if err:=unix.Flock(int(lock.Fd()),unix.LOCK_EX); err!=nil {
		return nil, fmt.Errorf("unix.Flock(): %w",err)
	}

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
	g := &logGuard{
		lock:lock,
		headPath:headPath,
	}
	if minIdx <= maxIdx {
		g.firstIdx = minIdx
		g.nextIdx = maxIdx+1
	}
	return g,nil
}

func (g *logGuard) Close() {
	g.lock.Close()
}

func (g *logGuard) Rotate(cfg *Config) error {
	// Move head to tail.
	if err:=os.Rename(g.headPath,tailPath(g.headPath,g.nextIdx)); err!=nil {
		return fmt.Errorf("os.Rename(): %w",err)
	}
	g.nextIdx++
	// truncate tail to acceptable size.
	if cfg.TotalSizeLimit<=0 {
		return nil
	}
	sizes := make([]int64,g.nextIdx-g.firstIdx)
	total := int64(0)
	for i := range sizes {
		size,err := fileSize(tailPath(g.headPath,g.firstIdx+i))
		if err!=nil { return err }
		sizes[i] = size
		total += size
	}
	for i:=0; i<len(sizes) && total>cfg.TotalSizeLimit; i++ {
		if err:=os.Remove(tailPath(g.headPath,g.firstIdx)); err!=nil {
			return err
		}
		g.firstIdx += 1
		total -= sizes[i]
	}
	return nil
}

func (g *logGuard) PathByOffset(fileOffset int) string {
	if fileOffset==0 {
		return g.headPath
	}
	return tailPath(g.headPath,g.nextIdx+fileOffset)
}

//--------------------------------------------------------------------------------

// LogReader provides an interface for reading from the log.
type LogReader struct {
	guard *logGuard
	fileOffset int
	file *fileReader
}

func NewLogReader(headPath string) (r *LogReader, resErr error) {
	g,err := newLogGuard(headPath)
	if err!=nil { return nil,err }
	defer func(){ if resErr!=nil { g.Close() } }()	
	f,err := newFileReader(g.headPath)
	if err!=nil { return nil,err }
	return &LogReader{
		guard: g,	
		fileOffset: 0,
		file: f,
	},nil
}

func (r *LogReader) SeekOffset(fileOffset int) error {
	if wantMin:=r.guard.nextIdx-r.guard.firstIdx; fileOffset<wantMin || fileOffset > 0 {
		return ErrInvalidOffset{fmt.Errorf("invalid offset %d, available range is [%d,0]",fileOffset,wantMin)}
	}
	r.fileOffset = fileOffset
	f,err := newFileReader(r.guard.PathByOffset(r.fileOffset))
	if err!=nil {
		return fmt.Errorf("newFileReader(): %w",err)
	}
	r.file.Close()
	r.file = f
	return nil
}

func (r *LogReader) Close() {
	r.file.Close()
	r.guard.Close()
}

// Read reads the next entry from the log.
// Returns io.EOF when the end of the log is reached.
func (r *LogReader) Read() ([]byte,error) {
	for {
		data,err := r.file.Read()
		if err==nil {
			return data,nil
		}
		if r.fileOffset==0 {
			// Last entry of the last file may be truncated because file writes are not atomic.
			// TODO(gprusak): they COULD be atomic, if we used O_APPEND when writing AND used custom buffering.
			if errors.Is(err,errEOF) || errors.Is(err,errTruncated) {
				return nil,io.EOF 
			}
			return nil,err
		} 
		if !errors.Is(err,errEOF) {
			return nil,err
		}
		// Open the next file and retry.
		if err := r.SeekOffset(r.fileOffset+1); err!=nil {
			return nil,err
		}
	}	
}
