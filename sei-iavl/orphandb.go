package iavl

import (
	"fmt"
	"io/fs"
	"io/ioutil"
	"os"
	"path"
	"strings"
)

type orphanDB struct {
	cache             map[int64]map[string]int64 // key: version, value: orphans
	directory         string
	numOrphansPerFile int
}

func NewOrphanDB(opts *Options) *orphanDB {
	return &orphanDB{
		cache:             map[int64]map[string]int64{},
		directory:         opts.OrphanDirectory,
		numOrphansPerFile: opts.NumOrphansPerFile,
	}
}

func (o *orphanDB) SaveOrphans(version int64, orphans map[string]int64) error {
	o.cache[version] = orphans
	chunks := [][]string{{}}
	for orphan := range orphans {
		if len(chunks[len(chunks)-1]) == o.numOrphansPerFile {
			chunks = append(chunks, []string{})
		}
		chunks[len(chunks)-1] = append(chunks[len(chunks)-1], orphan)
	}
	dir := path.Join(o.directory, fmt.Sprintf("%d", version))
	os.RemoveAll(dir)
	os.MkdirAll(dir, fs.ModePerm)
	for i, chunk := range chunks {
		f, err := os.Create(path.Join(dir, fmt.Sprintf("%d", i)))
		if err != nil {
			return err
		}
		f.WriteString(strings.Join(chunk, "\n"))
		f.Close()
	}
	return nil
}

func (o *orphanDB) GetOrphans(version int64) map[string]int64 {
	if _, ok := o.cache[version]; !ok {
		o.cache[version] = map[string]int64{}
		dir := path.Join(o.directory, fmt.Sprintf("%d", version))
		files, err := ioutil.ReadDir(dir)
		if err != nil {
			// no orphans found
			return o.cache[version]
		}
		for _, file := range files {
			content, err := ioutil.ReadFile(path.Join(dir, file.Name()))
			if err != nil {
				return o.cache[version]
			}
			for _, orphan := range strings.Split(string(content), "\n") {
				o.cache[version][orphan] = version
			}
		}
	}
	return o.cache[version]
}

func (o *orphanDB) DeleteOrphans(version int64) error {
	delete(o.cache, version)
	dir := path.Join(o.directory, fmt.Sprintf("%d", version))
	return os.RemoveAll(dir)
}
