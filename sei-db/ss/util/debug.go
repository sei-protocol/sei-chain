package util

import (
	"io/fs"
	"os"
	"path/filepath"
)

func CreateFile(outputDir string, fileName string) (*os.File, error) {
	err := os.MkdirAll(outputDir, fs.ModePerm)
	if err != nil {
		return nil, err
	}
	filename := filepath.Join(outputDir, fileName)

	currentFile, err := os.Create(filename)
	if err != nil {
		return nil, err
	}

	return currentFile, nil
}
