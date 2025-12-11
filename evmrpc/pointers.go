package evmrpc

import (
	"context"
	"encoding/csv"
	"fmt"
	"os"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
)

type PointersAPI struct {
	keeper         *keeper.Keeper
	ctxProvider    func(int64) sdk.Context
	connectionType ConnectionType
}

func NewPointersAPI(k *keeper.Keeper, ctxProvider func(int64) sdk.Context, connectionType ConnectionType) *PointersAPI {
	return &PointersAPI{
		keeper:         k,
		ctxProvider:    ctxProvider,
		connectionType: connectionType,
	}
}

// PointersResponse is the response returned by the sei_pointers RPC method
type PointersResponse struct {
	Message  string `json:"message"`
	FilePath string `json:"filePath"`
}

// Pointers kicks off a goroutine that iterates over all pointers and outputs them to a CSV in /tmp
func (p *PointersAPI) Pointers(_ context.Context) (*PointersResponse, error) {
	startTime := time.Now()
	defer recordMetrics("sei_pointers", p.connectionType, startTime)

	// Generate a unique filename with timestamp
	timestamp := time.Now().Format("20060102_150405")
	filePath := fmt.Sprintf("/tmp/sei_pointers_%s.csv", timestamp)

	// Kick off a goroutine to do the work
	go func() {
		ctx := p.ctxProvider(LatestCtxHeight)

		// Create the CSV file
		file, err := os.Create(filePath)
		if err != nil {
			fmt.Printf("Error creating pointers CSV file: %v\n", err)
			return
		}
		defer file.Close()

		writer := csv.NewWriter(file)
		defer writer.Flush()

		// Write the header
		if err := writer.Write([]string{"RegistryValue", "ReverseRegistryValue", "Type"}); err != nil {
			fmt.Printf("Error writing CSV header: %v\n", err)
			return
		}

		// Iterate over all pointers and write to CSV
		p.keeper.IterateAllPointers(ctx, func(pointer string, pointee string, pointerType keeper.PointerType) bool {
			record := []string{pointer, pointee, string(pointerType)}
			if err := writer.Write(record); err != nil {
				fmt.Printf("Error writing CSV record: %v\n", err)
				return true // Stop iteration on error
			}
			return false // Continue iteration
		})

		fmt.Printf("Pointers CSV export completed: %s\n", filePath)
	}()

	return &PointersResponse{
		Message:  "Pointer export started in background",
		FilePath: filePath,
	}, nil
}

