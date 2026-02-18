# Sei Chain

## Code Style

### Go Formatting

All Go files must be `gofmt` compliant. After modifying any `.go` files, run:

```bash
gofmt -s -w <file>
```

Or verify compliance with:

```bash
gofmt -s -l .
```

This command should produce no output if all files are properly formatted.

## Benchmarking

See [benchmark/CLAUDE.md](benchmark/CLAUDE.md) for benchmark usage, environment variables, and comparison workflows.
