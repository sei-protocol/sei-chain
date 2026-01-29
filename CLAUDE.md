# Sei Chain

## Go Version

This project requires Go 1.25.6. Use gvm to switch versions:

```bash
source ~/.gvm/scripts/gvm && gvm use go1.25.6
```

Or in a subshell:

```bash
bash -l -c "source ~/.gvm/scripts/gvm && gvm use go1.25.6 && <command>"
```

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
