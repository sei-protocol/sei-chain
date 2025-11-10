package utils

import (
	"encoding/json"
	"testing"
	"time"
)

func TestJSON(t *testing.T) {
	var got, want struct{ X Duration }
	want.X = Duration(100 * time.Millisecond)
	j, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%s", j)
	if err := json.Unmarshal(j, &got); err != nil {
		t.Fatal(err)
	}
	if err := TestDiff(want, got); err != nil {
		t.Fatal(err)
	}
}
