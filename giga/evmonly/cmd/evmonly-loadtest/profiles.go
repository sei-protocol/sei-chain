package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"runtime/trace"
)

type profileSession struct {
	cpuFile      *os.File
	traceFile    *os.File
	heapPath     string
	cpuActive    bool
	traceActive  bool
	heapComplete bool
}

func startProfiles(cfg config) (*profileSession, error) {
	if cfg.cpuProfile == "" && cfg.heapProfile == "" && cfg.traceProfile == "" {
		return nil, nil
	}
	session := &profileSession{heapPath: cfg.heapProfile}
	if cfg.cpuProfile != "" {
		file, err := createProfileFile(cfg.cpuProfile)
		if err != nil {
			return nil, err
		}
		session.cpuFile = file
		if err := pprof.StartCPUProfile(file); err != nil {
			_ = session.Close()
			return nil, fmt.Errorf("start CPU profile: %w", err)
		}
		session.cpuActive = true
	}
	if cfg.traceProfile != "" {
		file, err := createProfileFile(cfg.traceProfile)
		if err != nil {
			_ = session.Close()
			return nil, err
		}
		session.traceFile = file
		if err := trace.Start(file); err != nil {
			_ = session.Close()
			return nil, fmt.Errorf("start runtime trace: %w", err)
		}
		session.traceActive = true
	}
	return session, nil
}

func createProfileFile(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, fmt.Errorf("create profile dir for %s: %w", path, err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600) //nolint:gosec // profile output path is an explicit CLI argument.
	if err != nil {
		return nil, fmt.Errorf("create profile file %s: %w", path, err)
	}
	return file, nil
}

func (p *profileSession) Close() error {
	if p == nil {
		return nil
	}
	var errs []error
	if p.traceActive {
		trace.Stop()
		p.traceActive = false
	}
	if p.cpuActive {
		pprof.StopCPUProfile()
		p.cpuActive = false
	}
	if p.cpuFile != nil {
		errs = append(errs, p.cpuFile.Close())
		p.cpuFile = nil
	}
	if p.traceFile != nil {
		errs = append(errs, p.traceFile.Close())
		p.traceFile = nil
	}
	if p.heapPath != "" && !p.heapComplete {
		errs = append(errs, writeHeapProfile(p.heapPath))
		p.heapComplete = true
	}
	return errors.Join(errs...)
}

func writeHeapProfile(path string) (err error) {
	file, err := createProfileFile(path)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, file.Close())
	}()
	runtime.GC()
	if err := pprof.WriteHeapProfile(file); err != nil {
		return fmt.Errorf("write heap profile %s: %w", path, err)
	}
	return nil
}

func finishProfiles(session *profileSession, err *error) {
	if closeErr := session.Close(); closeErr != nil {
		if *err != nil {
			fmt.Fprintf(os.Stderr, "evmonly-loadtest: write profile: %v\n", closeErr)
			return
		}
		*err = closeErr
	}
}
