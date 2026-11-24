package threading

import (
	"context"
	"testing"
)

func TestInterruptiblePush_Success(t *testing.T) {
	ch := make(chan int, 1)
	err := InterruptiblePush(t.Context(), ch, 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v := <-ch; v != 42 {
		t.Errorf("expected 42, got %d", v)
	}
}

func TestInterruptiblePush_ContextCancelled(t *testing.T) {
	ch := make(chan int)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	err := InterruptiblePush(ctx, ch, 42)
	if err == nil {
		t.Error("expected error from InterruptiblePush with cancelled context")
	}
}

func TestInterruptiblePull_Success(t *testing.T) {
	ch := make(chan int, 1)
	ch <- 42
	v, err := InterruptiblePull(t.Context(), ch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != 42 {
		t.Errorf("expected 42, got %d", v)
	}
}

func TestInterruptiblePull_ContextCancelled(t *testing.T) {
	ch := make(chan int)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := InterruptiblePull(ctx, ch)
	if err == nil {
		t.Error("expected error from InterruptiblePull with cancelled context")
	}
}

func TestInterruptiblePull_ValueReadyAfterContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	for range 100 {
		ch := make(chan int, 1)
		ch <- 42
		v, err := InterruptiblePull(ctx, ch)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if v != 42 {
			t.Fatalf("expected 42, got %d", v)
		}
	}
}

func TestInterruptiblePull_ChannelClosed(t *testing.T) {
	ch := make(chan int)
	close(ch)

	_, err := InterruptiblePull(t.Context(), ch)
	if err == nil {
		t.Error("expected error from InterruptiblePull on closed channel")
	}
}
