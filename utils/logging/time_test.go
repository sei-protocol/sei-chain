package logging

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tendermint/tendermint/libs/log"
)

type mockLogger struct {
	lastError string
}

func (l *mockLogger) Debug(msg string, keyVals ...interface{}) {}
func (l *mockLogger) Info(msg string, keyVals ...interface{})  {}
func (l *mockLogger) Error(msg string, keyVals ...interface{}) {
	l.lastError = msg
}
func (l *mockLogger) With(keyVals ...interface{}) log.Logger { return l }

func TestFastReturn(t *testing.T) {
	logger := mockLogger{}
	task := func() (bool, error) {
		return true, nil
	}
	after := 3 * time.Second
	res, err := LogIfNotDoneAfter(&logger, task, after, "test")
	require.Nil(t, err)
	require.True(t, res)
	require.Empty(t, logger.lastError)
}

func TestFastError(t *testing.T) {
	logger := mockLogger{}
	task := func() (bool, error) {
		return true, errors.New("test")
	}
	after := 3 * time.Second
	res, err := LogIfNotDoneAfter(&logger, task, after, "test")
	require.NotNil(t, err)
	require.Empty(t, res)
	require.Empty(t, logger.lastError)
}

func TestSlowReturn(t *testing.T) {
	logger := mockLogger{}
	task := func() (bool, error) {
		time.Sleep(2 * time.Second)
		return true, nil
	}
	after := 1 * time.Second
	res, err := LogIfNotDoneAfter(&logger, task, after, "test")
	require.Nil(t, err)
	require.True(t, res)
	require.Equal(t, fmt.Sprintf("test still not finished after %s", after), logger.lastError)
}

func TestSlowError(t *testing.T) {
	logger := mockLogger{}
	task := func() (bool, error) {
		time.Sleep(2 * time.Second)
		return true, errors.New("test")
	}
	after := 1 * time.Second
	res, err := LogIfNotDoneAfter(&logger, task, after, "test")
	require.NotNil(t, err)
	require.Empty(t, res)
	require.Equal(t, fmt.Sprintf("test still not finished after %s", after), logger.lastError)
}

func TestPanic(t *testing.T) {
	logger := mockLogger{}
	task := func() (bool, error) {
		panic("test")
	}
	after := 1 * time.Second
	outer := func() {
		defer func() {
			if err := recover(); err != nil {
			}
		}()
		LogIfNotDoneAfter(&logger, task, after, "test")
	}
	require.Panics(t, func() { LogIfNotDoneAfter(&logger, task, after, "test") })
	require.NotPanics(t, outer)
}
