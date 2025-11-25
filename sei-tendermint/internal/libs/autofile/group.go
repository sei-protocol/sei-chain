package autofile

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/tendermint/tendermint/libs/log"
	"github.com/tendermint/tendermint/libs/utils"
	"github.com/tendermint/tendermint/libs/service"
)

const (
	maxFilesToRemove          = 4                      // needs to be greater than 1
)

type Config struct {
	HeadSizeLimit      int64
	TotalSizeLimit     int64
	GroupCheckDuration time.Duration
}

func DefaultConfig() *Config {
	return &Config {
		HeadSizeLimit:       10 * 1024 * 1024,       // 10MB
		TotalSizeLimit:      1 * 1024 * 1024 * 1024, // 1GB
		GroupCheckDuration:  5000 * time.Millisecond,
	}
}

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
type Group struct {
	logger log.Logger
	config *Config
	headPath string
	
	mtx                sync.Mutex	
	minIndex           int // Includes head
	maxIndex           int // Includes head, where Head will move to

	// TODO: When we start deleting files, we need to start tracking GroupReaders
	// and their dependencies.
}

// OpenGroup creates a new Group with head at headPath. It returns an error if
// it fails to open head file.
func OpenGroup(ctx context.Context, logger log.Logger, headPath string, config *Config) (*Group, error) {
	g := &Group{
		headPath: headPath,
		logger:             logger,
		config: config,	
		minIndex:           0,
		maxIndex:           0,
	}
	gInfo := g.readGroupInfo()
	g.minIndex = gInfo.MinIndex
	g.maxIndex = gInfo.MaxIndex
	return g, nil
}

// OnStart implements service.Service by starting the goroutine that checks file
// and group limits.
func (g *Group) Run(ctx context.Context) error {
	dir, err := filepath.Abs(filepath.Dir(g.headPath))
	if err != nil {
		return err
	}
	head, err := OpenAutoFile(ctx, g.headPath)
	if err != nil {
		return err
	}
	headBuf := bufio.NewWriterSize(head, 4096*10)
	ticker := time.NewTicker(g.config.GroupCheckDuration)

	for {
		if len(cmd.data)>0 {
			if _,err := headBuf.Write(cmd.Data); err!=nil {
				return err
			}
		}
		if done,ok := cmd.sync.Get(); ok {
			if err := headBuf.Flush(); err!=nil {
				return err
			}
			if err := head.Sync(); err!=nil {
				return err
			}
			close(done)
		}
	}
	for {
		if _,err := utils.Recv(ctx,ticker.C); err!=nil {
			return err
		}
		g.checkHeadSizeLimit(ctx)
		g.checkTotalSizeLimit(ctx)
	}
}

// MaxIndex returns index of the last file in the group.
func (g *Group) MaxIndex() int {
	g.mtx.Lock()
	defer g.mtx.Unlock()
	return g.maxIndex
}

// MinIndex returns index of the first file in the group.
func (g *Group) MinIndex() int {
	g.mtx.Lock()
	defer g.mtx.Unlock()
	return g.minIndex
}

// Write writes the contents of p into the current head of the group. It
// returns the number of bytes written. If nn < len(p), it also returns an
// error explaining why the write is short.
// NOTE: Writes are buffered so they don't write synchronously
// TODO: Make it halt if space is unavailable
func (g *Group) Write(ctx context.Context, p []byte) error {
	return utils.Send(ctx,g.cmds,cmd{data:p})
}

// FlushAndSync writes any buffered data to the underlying file and commits the
// current content of the file to stable storage (fsync).
func (g *Group) Sync() error {
	done := make(chan struct{})
	if err:=utils.Send(ctx,g.cmds,cmd{sync:done}); err!=nil { return err }
	_,err :=utils.RecvOrClosed(ctx,done)
	return err
}

// NOTE: this function is called manually in tests.
func (g *Group) checkHeadSizeLimit(ctx context.Context) {
	limit := g.config.HeadSizeLimit
	if limit == 0 {
		return
	}
	size, err := g.Head.Size()
	if err != nil {
		g.logger.Error("Group's head may grow without bound", "head", g.Head.Path, "err", err)
		return
	}
	if size >= limit {
		g.rotateFile(ctx)
	}
}

func (g *Group) checkTotalSizeLimit(ctx context.Context) {
	g.mtx.Lock()
	defer g.mtx.Unlock()

	if err := ctx.Err(); err != nil {
		return
	}

	if g.totalSizeLimit == 0 {
		return
	}

	gInfo := g.readGroupInfo()
	totalSize := gInfo.TotalSize
	for i := range maxFilesToRemove {
		index := gInfo.MinIndex + i
		if totalSize < g.totalSizeLimit {
			return
		}
		if index == gInfo.MaxIndex {
			// Special degenerate case, just do nothing.
			g.logger.Error("Group's head may grow without bound", "head", g.Head.Path)
			return
		}

		if ctx.Err() != nil {
			return
		}

		pathToRemove := filePathForIndex(g.Head.Path, index, gInfo.MaxIndex)
		fInfo, err := os.Stat(pathToRemove)
		if err != nil {
			g.logger.Error("Failed to fetch info for file", "file", pathToRemove)
			continue
		}

		if ctx.Err() != nil {
			return
		}

		if err = os.Remove(pathToRemove); err != nil {
			g.logger.Error("Failed to remove path", "path", pathToRemove)
			return
		}
		totalSize -= fInfo.Size()
	}
}

// rotateFile causes group to close the current head and assign it
// some index. Panics if it encounters an error.
func (g *Group) rotateFile(ctx context.Context) {
	g.mtx.Lock()
	defer g.mtx.Unlock()

	if err := ctx.Err(); err != nil {
		return
	}

	headPath := g.Head.Path

	if err := g.headBuf.Flush(); err != nil {
		panic(err)
	}
	if err := g.Head.Sync(); err != nil {
		panic(err)
	}
	err := g.Head.withLock(func() error {
		if err := ctx.Err(); err != nil {
			return err
		}

		if err := g.Head.unsyncCloseFile(); err != nil {
			return err
		}

		indexPath := filePathForIndex(headPath, g.maxIndex, g.maxIndex+1)
		return os.Rename(headPath, indexPath)
	})
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return
	}
	if err != nil {
		panic(err)
	}

	g.maxIndex++
}

// NewReader returns a new group reader.
// CONTRACT: Caller must close the returned GroupReader.
func (g *Group) NewReader(index int) (*GroupReader, error) {
	r := newGroupReader(g)
	err := r.SetIndex(index)
	if err != nil {
		return nil, err
	}
	return r, nil
}

// GroupInfo holds information about the group.
type GroupInfo struct {
	MinIndex  int   // index of the first file in the group, including head
	MaxIndex  int   // index of the last file in the group, including head
	TotalSize int64 // total size of the group
	HeadSize  int64 // size of the head
}

// Returns info after scanning all files in g.Head's dir.
func (g *Group) ReadGroupInfo() GroupInfo {
	g.mtx.Lock()
	defer g.mtx.Unlock()
	return g.readGroupInfo()
}

// Index includes the head.
// CONTRACT: caller should have called g.mtx.Lock
func (g *Group) readGroupInfo() GroupInfo {
	groupDir := filepath.Dir(g.Head.Path)
	headBase := filepath.Base(g.Head.Path)
	var minIndex, maxIndex int = -1, -1
	var totalSize, headSize int64 = 0, 0

	dir, err := os.Open(groupDir)
	if err != nil {
		panic(err)
	}
	defer dir.Close()
	fiz, err := dir.Readdir(0)
	if err != nil {
		panic(err)
	}

	// For each file in the directory, filter by pattern
	for _, fileInfo := range fiz {
		if fileInfo.Name() == headBase {
			fileSize := fileInfo.Size()
			totalSize += fileSize
			headSize = fileSize
			continue
		} else if strings.HasPrefix(fileInfo.Name(), headBase) {
			fileSize := fileInfo.Size()
			totalSize += fileSize
			indexedFilePattern := regexp.MustCompile(`^.+\.([0-9]{3,})$`)
			submatch := indexedFilePattern.FindSubmatch([]byte(fileInfo.Name()))
			if len(submatch) != 0 {
				// Matches
				fileIndex, err := strconv.Atoi(string(submatch[1]))
				if err != nil {
					panic(err)
				}
				if maxIndex < fileIndex {
					maxIndex = fileIndex
				}
				if minIndex == -1 || fileIndex < minIndex {
					minIndex = fileIndex
				}
			}
		}
	}

	// Now account for the head.
	if minIndex == -1 {
		// If there were no numbered files,
		// then the head is index 0.
		minIndex, maxIndex = 0, 0
	} else {
		// Otherwise, the head file is 1 greater
		maxIndex++
	}
	return GroupInfo{minIndex, maxIndex, totalSize, headSize}
}

func filePathForIndex(headPath string, index int, maxIndex int) string {
	if index == maxIndex {
		return headPath
	}
	return fmt.Sprintf("%v.%03d", headPath, index)
}

//--------------------------------------------------------------------------------

// GroupReader provides an interface for reading from a Group.
type GroupReader struct {
	*Group
	mtx       sync.Mutex
	curIndex  int
	curFile   *os.File
	curReader *bufio.Reader
	curLine   []byte
}

func newGroupReader(g *Group) *GroupReader {
	return &GroupReader{
		Group:     g,
		curIndex:  0,
		curFile:   nil,
		curReader: nil,
		curLine:   nil,
	}
}

// Close closes the GroupReader by closing the cursor file.
func (gr *GroupReader) Close() error {
	gr.mtx.Lock()
	defer gr.mtx.Unlock()

	if gr.curReader != nil {
		err := gr.curFile.Close()
		gr.curIndex = 0
		gr.curReader = nil
		gr.curFile = nil
		gr.curLine = nil
		return err
	}
	return nil
}

// Read implements io.Reader, reading bytes from the current Reader
// incrementing index until enough bytes are read.
func (gr *GroupReader) Read(p []byte) (n int, err error) {
	lenP := len(p)
	if lenP == 0 {
		return 0, errors.New("given empty slice")
	}

	gr.mtx.Lock()
	defer gr.mtx.Unlock()

	// Open file if not open yet
	if gr.curReader == nil {
		if err = gr.openFile(gr.curIndex); err != nil {
			return 0, err
		}
	}

	// Iterate over files until enough bytes are read
	var nn int
	for {
		nn, err = gr.curReader.Read(p[n:])
		n += nn
		switch {
		case err == io.EOF:
			if n >= lenP {
				return n, nil
			}
			// Open the next file
			if err1 := gr.openFile(gr.curIndex + 1); err1 != nil {
				return n, err1
			}
		case err != nil:
			return n, err
		case nn == 0: // empty file
			return n, err
		}
	}
}

// IF index > gr.Group.maxIndex, returns io.EOF
// CONTRACT: caller should hold gr.mtx
func (gr *GroupReader) openFile(index int) error {
	// Lock on Group to ensure that head doesn't move in the meanwhile.
	gr.Group.mtx.Lock()
	defer gr.Group.mtx.Unlock()

	if index > gr.Group.maxIndex {
		return io.EOF
	}

	curFilePath := filePathForIndex(gr.Head.Path, index, gr.Group.maxIndex)
	curFile, err := os.OpenFile(curFilePath, os.O_RDONLY|os.O_CREATE, autoFilePerms)
	if err != nil {
		return err
	}
	curReader := bufio.NewReader(curFile)

	// Update gr.cur*
	if gr.curFile != nil {
		gr.curFile.Close() // TODO return error?
	}
	gr.curIndex = index
	gr.curFile = curFile
	gr.curReader = curReader
	gr.curLine = nil
	return nil
}

// CurIndex returns cursor's file index.
func (gr *GroupReader) CurIndex() int {
	gr.mtx.Lock()
	defer gr.mtx.Unlock()
	return gr.curIndex
}

// SetIndex sets the cursor's file index to index by opening a file at this
// position.
func (gr *GroupReader) SetIndex(index int) error {
	gr.mtx.Lock()
	defer gr.mtx.Unlock()
	return gr.openFile(index)
}
