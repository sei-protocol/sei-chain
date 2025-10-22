package utils

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestERCMetadata(t *testing.T) {
	tests := []struct {
		name     string
		metadata ERCMetadata
		expected ERCMetadata
	}{
		{
			name: "Valid ERC-20 metadata",
			metadata: ERCMetadata{
				Name:     "Test Token",
				Symbol:   "TEST",
				Decimals: 18,
			},
			expected: ERCMetadata{
				Name:     "Test Token",
				Symbol:   "TEST",
				Decimals: 18,
			},
		},
		{
			name: "Zero decimals token",
			metadata: ERCMetadata{
				Name:     "No Decimal Token",
				Symbol:   "NDT",
				Decimals: 0,
			},
			expected: ERCMetadata{
				Name:     "No Decimal Token",
				Symbol:   "NDT",
				Decimals: 0,
			},
		},
		{
			name: "Empty metadata",
			metadata: ERCMetadata{
				Name:     "",
				Symbol:   "",
				Decimals: 0,
			},
			expected: ERCMetadata{
				Name:     "",
				Symbol:   "",
				Decimals: 0,
			},
		},
		{
			name: "High decimals token",
			metadata: ERCMetadata{
				Name:     "High Precision Token",
				Symbol:   "HPT",
				Decimals: 255, // max uint8
			},
			expected: ERCMetadata{
				Name:     "High Precision Token",
				Symbol:   "HPT",
				Decimals: 255,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.expected.Name, tt.metadata.Name)
			require.Equal(t, tt.expected.Symbol, tt.metadata.Symbol)
			require.Equal(t, tt.expected.Decimals, tt.metadata.Decimals)
		})
	}
}

func TestERCMetadataFields(t *testing.T) {
	metadata := ERCMetadata{
		Name:     "Sei Token",
		Symbol:   "SEI",
		Decimals: 6,
	}

	// Test individual field access
	require.Equal(t, "Sei Token", metadata.Name)
	require.Equal(t, "SEI", metadata.Symbol)
	require.Equal(t, uint8(6), metadata.Decimals)

	// Test field modification
	metadata.Name = "Modified Name"
	metadata.Symbol = "MOD"
	metadata.Decimals = 12

	require.Equal(t, "Modified Name", metadata.Name)
	require.Equal(t, "MOD", metadata.Symbol)
	require.Equal(t, uint8(12), metadata.Decimals)
}

func TestERCMetadataZeroValue(t *testing.T) {
	var metadata ERCMetadata

	require.Equal(t, "", metadata.Name)
	require.Equal(t, "", metadata.Symbol)
	require.Equal(t, uint8(0), metadata.Decimals)
}

func TestERCMetadataComparison(t *testing.T) {
	metadata1 := ERCMetadata{
		Name:     "Token A",
		Symbol:   "TKA",
		Decimals: 18,
	}

	metadata2 := ERCMetadata{
		Name:     "Token A",
		Symbol:   "TKA",
		Decimals: 18,
	}

	metadata3 := ERCMetadata{
		Name:     "Token B",
		Symbol:   "TKB",
		Decimals: 18,
	}

	require.Equal(t, metadata1, metadata2)
	require.NotEqual(t, metadata1, metadata3)
}
