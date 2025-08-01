package service

import (
	"errors"
	"testing"
)

func TestParallelOk(t *testing.T) {
	x := [10]int{}
	if err := Parallel(func(s ParallelScope) error {
		for i := range x {
			s.Spawn(func() error {
				x[i] = i
				return nil
			})
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for want, got := range x {
		if want != got {
			t.Fatalf("x[%d] = %d, want %d", want, got, want)
		}
	}
}

func TestParallelFail(t *testing.T) {
	var wantErr = errors.New("custom err")
	x := [10]int{}
	err := Parallel(func(s ParallelScope) error {
		for i := range x {
			s.Spawn(func() error {
				if i%2 == 0 {
					return wantErr
				}
				x[i] = i
				return nil
			})
		}
		return nil
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
	for want, got := range x {
		if want%2 == 0 {
			want = 0
		}
		if want != got {
			t.Fatalf("x[%d] = %d, want %d", want, got, want)
		}
	}
}
