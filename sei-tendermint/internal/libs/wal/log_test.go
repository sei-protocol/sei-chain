package wal 

import (
	"fmt"
	"path"
	"testing"

	"github.com/tendermint/tendermint/libs/utils"
	"github.com/tendermint/tendermint/libs/utils/require"
)

func (l *Log) minOffset() int {
	for inner := range l.inner.Lock() {
		if inner,ok := inner.Get(); ok {
			return inner.view.firstIdx-inner.view.nextIdx
		}
	}
	return 0
}

func OrPanic1[T any](v T, err error) T {
	if err!=nil {
		panic(err)
	}
	return v
}

func TestReadWrite(t *testing.T) {
	for _,reopen := range utils.Slice(true,false) {
		t.Run(fmt.Sprintf("reopen=%v",reopen), func(t *testing.T) {
			rng := utils.TestRng()
			headPath := path.Join(t.TempDir(),"testlog")
			cfg := &Config{FileSizeLimit:1000}
			var want [][]byte
			t.Logf("Open a log")
			l := OrPanic1(NewLog(headPath,cfg))
			defer func() { l.Close() }()
			for it := range 5 {
				t.Logf("ITERATION %v",it)
				if reopen {
					l.Close()
					l = OrPanic1(NewLog(headPath,cfg))
				}
				t.Logf("Opening a log again should fail - previous instance holds a lock on it.")
				_,err := NewLog(headPath,cfg)
				require.Error(t,err)
				t.Logf("Append a bunch of random entries.")
				require.NoError(t,l.OpenForAppend())
				for range 400 {
					entry := utils.GenBytes(rng, rng.Intn(50)+10)
					want = append(want,entry)
					require.NoError(t, l.Append(entry))
				}
				t.Logf("Sync the log and close.")
				require.NoError(t,l.Sync())
				
				t.Logf("Read entries.")
				if reopen {
					l.Close()
					l = OrPanic1(NewLog(headPath,cfg))
				}
				require.NoError(t,l.OpenForRead(l.minOffset()))
				for _,wantE := range want {
					gotE,err := l.Read()
					require.NoError(t,err)
					require.NoError(t,utils.TestDiff(wantE,gotE))
				}
			}
		})
	}
}
