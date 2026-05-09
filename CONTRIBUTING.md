# Contributing to go-magic

Thank you for your interest in contributing to go-magic!

## How to Contribute

### Reporting Bugs

Before creating a bug report:
- Search existing issues to avoid duplicates
- Use the [Bug Report template](./.github/ISSUE_TEMPLATE/bug_report.yml)
- Include your Go version, OS, and relevant configuration
- Provide clear reproduction steps

### Suggesting Features

- Search existing issues first
- Use the [Feature Request template](./.github/ISSUE_TEMPLATE/feature_request.yml)
- Explain the use case and why it would benefit the project

### Pull Requests

1. **Fork & Clone**
   ```bash
   git clone https://github.com/YOUR_USERNAME/go-magic.git
   cd go-magic
   ```

2. **Create a Branch**
   ```bash
   git checkout -b feat/your-feature-name
   # or
   git checkout -b fix/your-bug-fix
   ```

3. **Development Setup**
   ```bash
   # Install dependencies
   make deps

   # Run linter
   make lint

   # Run tests
   make test
   ```

4. **Make Your Changes**
   - Follow the existing code style
   - Write tests for new functionality
   - Keep commits atomic and well-described
   - Update documentation if needed

5. **Commit Format**
   ```
   type(scope): short description

   Detailed explanation (if needed)

   Fixes #issue-number
   ```

   Types: `feat`, `fix`, `docs`, `style`, `refactor`, `test`, `chore`, `build`

6. **Push & Create PR**
   ```bash
   git push origin your-branch-name
   ```

   Then open a Pull Request on GitHub.

### Code Review Process

- All PRs require at least one maintainer review
- Address review feedback by pushing new commits
- Once approved, a maintainer will merge

### Style Guide

- Run `make fmt` before committing
- Run `make lint` to check for issues
- Run `make vet` for additional checks
- Follow [Effective Go](https://go.dev/doc/effective_go)

### Testing

```bash
# Run all tests
make test

# Run with coverage
make test-coverage

# Run specific test
go test ./pkg/config/... -v
```

## License

By contributing, you agree that your contributions will be licensed under the MIT License.
