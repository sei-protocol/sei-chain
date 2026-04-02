package utils

import (
	"encoding/binary"
	"fmt"
	"io"
	"io/fs"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	dbm "github.com/tendermint/tm-db"
)

type KeyValuePair struct {
	Key   []byte `json:"key"`
	Value []byte `json:"value"`
}

// Opens application db
func OpenDB(dir string) (dbm.DB, error) {
	switch {
	case strings.HasSuffix(dir, ".db"):
		dir = dir[:len(dir)-3]
	case strings.HasSuffix(dir, ".db/"):
		dir = dir[:len(dir)-4]
	default:
		return nil, fmt.Errorf("database directory must end with .db")
	}
	// TODO: doesn't work on windows!
	cut := strings.LastIndex(dir, "/")
	if cut == -1 {
		return nil, fmt.Errorf("cannot cut paths on %s", dir)
	}
	name := dir[cut+1:]
	db, err := dbm.NewGoLevelDB(name, dir[:cut])
	if err != nil {
		return nil, err
	}
	return db, nil
}

// Reads raw keys / values from a file
func ReadKVEntriesFromFile(filename string) ([]KeyValuePair, error) {
	f, err := os.Open(filepath.Clean(filename))
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var kvPairs []KeyValuePair
	for {
		key, err := readByteSlice(f)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		value, err := readByteSlice(f)
		if err != nil {
			return nil, err
		}

		kvPairs = append(kvPairs, KeyValuePair{Key: key, Value: value})
	}

	return kvPairs, nil
}

func readByteSlice(r io.Reader) ([]byte, error) {
	var length uint32
	if err := binary.Read(r, binary.LittleEndian, &length); err != nil {
		return nil, err
	}

	data := make([]byte, length)
	_, err := io.ReadFull(r, data)
	return data, err
}

// NOTE: Assumes latencies is sorted
// CalculatePercentile calculates latencies percentile
func CalculatePercentile(latencies []time.Duration, percentile float64) time.Duration {
	if percentile < 0 || percentile > 100 {
		panic(fmt.Sprintf("Invalid percentile: %f", percentile))
	}
	index := int(float64(len(latencies)-1) * percentile / 100.0)
	return latencies[index]
}

// Picks random file from input kv dir and updates processedFiles Map with it
func PickRandomKVFile(inputKVDir string, processedFiles *sync.Map) string {
	files, _ := os.ReadDir(inputKVDir)
	var availableFiles []string

	for _, file := range files {
		if _, processed := processedFiles.Load(file.Name()); !processed && strings.HasSuffix(file.Name(), ".kv") {
			availableFiles = append(availableFiles, file.Name())
		}
	}

	if len(availableFiles) == 0 {
		return ""
	}

	selected := availableFiles[rand.Intn(len(availableFiles))]
	processedFiles.Store(selected, true)
	return selected
}

func ListAllFiles(dir string) ([]string, error) {
	files, err := os.ReadDir(dir)
	if err != nil {
		return []string{}, err
	}
	// Extract file nams from input KV dir
	fileNames := make([]string, 0, len(files))
	for _, file := range files {
		fileNames = append(fileNames, file.Name())
	}

	return fileNames, nil
}

func LoadAndShuffleKV(inputDir string, concurrency int) ([]KeyValuePair, error) {
	var allKVs []KeyValuePair
	mu := &sync.Mutex{}

	allFiles, err := ListAllFiles(inputDir)
	if err != nil {
		log.Fatalf("Failed to list all files: %v", err)
	}

	filesChan := make(chan string)
	wg := &sync.WaitGroup{}

	// Start worker goroutines
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for selectedFile := range filesChan {
				kvEntries, err := ReadKVEntriesFromFile(filepath.Join(inputDir, selectedFile))
				if err != nil {
					panic(err)
				}

				// Safely append the kvEntries to allKVs
				mu.Lock()
				allKVs = append(allKVs, kvEntries...)
				fmt.Printf("Done processing file %+v\n", filepath.Join(inputDir, selectedFile))
				mu.Unlock()
			}
		}()
	}

	// Send file names to filesChan
	for _, file := range allFiles {
		filesChan <- file
	}
	close(filesChan)

	// Wait for all workers to finish
	wg.Wait()

	rand.Shuffle(len(allKVs), func(i, j int) {
		allKVs[i], allKVs[j] = allKVs[j], allKVs[i]
	})

	return allKVs, nil
}

func CreateFile(outputDir string, fileName string) (*os.File, error) {
	outputDir = filepath.Clean(outputDir)
	fileName = filepath.Clean(fileName)
	err := os.MkdirAll(outputDir, fs.ModePerm)
	if err != nil {
		return nil, err
	}
	filename := filepath.Clean(filepath.Join(outputDir, fileName))

	currentFile, err := os.Create(filename)
	if err != nil {
		return nil, err
	}

	return currentFile, nil
}
