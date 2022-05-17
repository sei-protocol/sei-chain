#!/bin/bash

# Custom command to execute simulation_test.go
go test -benchmem -run=^$ -bench ^BenchmarkSimulation -NumBlocks=200 -BlockSize 50 -Commit=true -Verbose=true -Enabled=true