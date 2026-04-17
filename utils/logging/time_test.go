package logging

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestFastReturn(t *testing.T) {
	task := func() (bool, error) {
		return true, nil
	}
	after := 3 * time.Second
	res, err := LogIfNotDoneAfter(task, after, "test")
	require.Nil(t, err)
	require.True(t, res)
}

func TestFastError(t *testing.T) {
	task := func() (bool, error) {
		return true, errors.New("test")
	}
	after := 3 * time.Second
	res, err := LogIfNotDoneAfter(task, after, "test")
	require.NotNil(t, err)
	require.Empty(t, res)
}

func TestSlowReturn(t *testing.T) {
	task := func() (bool, error) {
		time.Sleep(2 * time.Second)
		return true, nil
	}
	after := 1 * time.Second
	res, err := LogIfNotDoneAfter(task, after, "test")
	require.Nil(t, err)
	require.True(t, res)
}

func TestSlowError(t *testing.T) {
	task := func() (bool, error) {
		time.Sleep(2 * time.Second)
		return true, errors.New("test")
	}
	after := 1 * time.Second
	res, err := LogIfNotDoneAfter(task, after, "test")
	require.NotNil(t, err)
	require.Empty(t, res)
}

func TestPanic(t *testing.T) {
	task := func() (bool, error) {
		panic("test")
	}
	after := 1 * time.Second
	outer := func() {
		defer func() {
			if err := recover(); err != nil {
			}
		}()
		LogIfNotDoneAfter(task, after, "test")
	}
	require.Panics(t, func() { LogIfNotDoneAfter(task, after, "test") })
	require.NotPanics(t, outer)
}
