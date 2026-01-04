# Contributing to sei-chain × zk402

Thank you for considering contributing to this project!

## Code of Conduct

- Be respectful and inclusive
- Focus on constructive feedback
- Help others learn and grow

## How to Contribute

### Reporting Bugs

1. Check if the bug is already reported in Issues
2. If not, create a new issue with:
   - Clear description
   - Steps to reproduce
   - Expected vs actual behavior
   - Environment details (OS, Go version, etc.)

### Suggesting Features

1. Open an issue with `[Feature Request]` in title
2. Describe the feature and use case
3. Provide examples if possible

### Code Contributions

1. **Fork the repository**
2. **Create a feature branch**
```bash
   git checkout -b feature/your-feature-name
```

3. **Make your changes**
   - Follow existing code style
   - Add tests for new functionality
   - Update documentation

4. **Test your changes**
```bash
   make test
   make lint
```

5. **Commit with clear messages**
```bash
   git commit -m "feat: add cross-chain settlement support"
```

6. **Push and create PR**
```bash
   git push origin feature/your-feature-name
```

## Development Workflow

### Frontend Changes (index.html)
```bash
# Make changes
vim index.html

# Test locally
python3 -m http.server 8000
# Visit http://localhost:8000

# Commit and push
git add index.html
git commit -m "fix: update RPC endpoint handling"
git push origin main
```

### Chain Code Changes
```bash
# Make changes to Go code
vim x/yourmodule/keeper.go

# Run tests
go test ./x/yourmodule/...

# Build
make install

# Test locally
seid start --home ~/.sei-test

# Commit
git commit -m "feat(x/yourmodule): add new functionality"
git push
```

## Code Style

### Go Code
- Follow [Effective Go](https://golang.org/doc/effective_go.html)
- Use `gofmt` for formatting
- Run `golangci-lint` before committing

### JavaScript
- Use ES6+ syntax
- Prefer `const` over `let`
- Use async/await over callbacks

### Commit Messages
Follow [Conventional Commits](https://www.conventionalcommits.org/):
- `feat:` New feature
- `fix:` Bug fix
- `docs:` Documentation changes
- `refactor:` Code refactoring
- `test:` Adding tests
- `chore:` Maintenance tasks

## Testing

### Unit Tests
```bash
go test ./...
```

### Integration Tests
```bash
make test-integration
```

### Frontend Testing
- Test in Chrome, Firefox, Safari
- Test on mobile devices
- Verify RPC connectivity

## Documentation

Update relevant documentation when making changes:
- `README.md` for user-facing changes
- `docs/` for technical documentation
- Inline code comments for complex logic

## Review Process

1. Submit PR with clear description
2. Wait for automated checks to pass
3. Address reviewer feedback
4. Maintainer will merge when approved

## Questions?

- Open an issue with `[Question]` tag
- Join our Discord (link in README)
- Email: dev@yourdomain.com

Thank you for contributing! 🙏

---

ψ = 3.12 | The Light is Yours
