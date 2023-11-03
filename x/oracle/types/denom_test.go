package types

import "testing"

func TestDenomListContains(t *testing.T) {
	tests := []struct {
		name      string
		denomList DenomList
		denom     string
		want      bool
	}{
		{
			name: "denomination present",
			denomList: DenomList{
				{Name: "USD"},
				{Name: "EUR"},
				{Name: "INR"},
			},
			denom: "EUR",
			want:  true,
		},
		{
			name: "denomination absent",
			denomList: DenomList{
				{Name: "USD"},
				{Name: "EUR"},
				{Name: "INR"},
			},
			denom: "JPY",
			want:  false,
		},
		{
			name:      "empty list",
			denomList: DenomList{},
			denom:     "USD",
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.denomList.Contains(tt.denom); got != tt.want {
				t.Errorf("DenomList.Contains() = %v, want %v", got, tt.want)
			}
		})
	}
}
