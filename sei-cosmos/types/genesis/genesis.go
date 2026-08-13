package genesis

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
)

type GenesisImportConfig struct {
	StreamGenesisImport bool
	GenesisStreamFile   string
}

const bufferSize = 100000

func IngestGenesisFileLineByLine(filename string) <-chan string {
	lines := make(chan string)

	go func() {
		defer close(lines)

		file, err := os.Open(filename)
		if err != nil {
			fmt.Println("Error opening file:", err)
			return
		}
		defer file.Close()

		reader := bufio.NewReader(file)

		buffer := make([]byte, bufferSize)
		lineBuf := new(strings.Builder)

		for {
			bytesRead, err := reader.Read(buffer)
			if err != nil && err != io.EOF {
				fmt.Println("Error reading file:", err)
				return
			}

			chunk := buffer[:bytesRead]
			for len(chunk) > 0 {
				i := bytes.IndexByte(chunk, '\n')
				if i >= 0 {
					lineBuf.Write(chunk[:i])
					lines <- lineBuf.String()
					lineBuf.Reset()
					chunk = chunk[i+1:]
				} else {
					lineBuf.Write(chunk)
					break
				}
			}

			if err == io.EOF {
				if lineBuf.Len() > 0 {
					lines <- lineBuf.String()
				}
				break
			}
		}
	}()

	return lines
}
