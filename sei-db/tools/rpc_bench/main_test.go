package main

import (
	"reflect"
	"strings"
	"testing"
)

func TestBuildBlockNumbers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		latestBlock int64
		blockCount  int
		startBlock  int64
		endBlock    int64
		want        []int64
		wantErr     string
	}{
		{
			name:        "recent blocks",
			latestBlock: 10,
			blockCount:  3,
			want:        []int64{10, 9, 8},
		},
		{
			name:        "recent blocks stop at genesis",
			latestBlock: 2,
			blockCount:  5,
			want:        []int64{2, 1},
		},
		{
			name:        "single explicit block defaults end",
			latestBlock: 10,
			startBlock:  4,
			want:        []int64{4},
		},
		{
			name:        "explicit range",
			latestBlock: 10,
			startBlock:  4,
			endBlock:    6,
			want:        []int64{4, 5, 6},
		},
		{
			name:        "explicit range requires start",
			latestBlock: 10,
			endBlock:    6,
			wantErr:     "start-block is required",
		},
		{
			name:        "explicit range rejects reversed bounds",
			latestBlock: 10,
			startBlock:  7,
			endBlock:    6,
			wantErr:     "cannot be greater",
		},
		{
			name:        "explicit range rejects future block",
			latestBlock: 10,
			startBlock:  7,
			endBlock:    11,
			wantErr:     "cannot exceed latest block",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := buildBlockNumbers(tt.latestBlock, tt.blockCount, tt.startBlock, tt.endBlock)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %q", tt.wantErr, err.Error())
				}
				return
			}

			if err != nil {
				t.Fatalf("buildBlockNumbers returned error: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("buildBlockNumbers mismatch: got %v want %v", got, tt.want)
			}
		})
	}
}
