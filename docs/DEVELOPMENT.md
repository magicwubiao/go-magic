# Development Guide

This guide covers local development environment setup, code standards, testing, and the submission process.

## Table of Contents

- [Prerequisites](#prerequisites)
- [Environment Setup](#environment-setup)
- [Code Standards](#code-standards)
- [Testing](#testing)
- [Submission Process](#submission-process)
- [CI/CD](#cicd)

## Prerequisites

- Go 1.21 or higher
- Git
- Make
- SQLite (for runtime)

## Environment Setup

### 1. Clone the Repository

```bash
git clone https://github.com/magicwubiao/go-magic.git
cd go-magic
```

### 2. Install Dependencies

```bash
go mod download
```

### 3. Build the Project

```bash
# Build the CLI
make build

# Or build manually
go build -o magic ./cmd/magic
```

### 4. Run Tests

```bash
# Run all tests
make test

# Run with coverage
make cover

# Run specific test
go test ./internal/cortex/... -v
```

### 5. Run Linters

```bash
# Install linters
make lint-install

# Run linters
make lint
```

## Code Standards

### Go Style Guide

Follow the official [Go Code Review Comments](https://github.com/golang/go/wiki/CodeReviewComments) and [Effective Go](https://go.dev/doc/effective_go).

### Naming Conventions

| Element | Convention | Example |
|---------|------------|---------|
| Packages | lowercase, no underscores | `cortex`, `agent` |
| Types | PascalCase | `CortexManager`, `AgentConfig` |
| Interfaces | PascalCase, often with -er suffix | `Plugin`, `Tool`, `Executor` |
| Functions | PascalCase | `NewManager`, `ProcessMessage` |
| Variables | camelCase | `maxTurns`, `taskPlan` |
| Constants | PascalCase | `MaxRetries`, `DefaultTimeout` |
| Private vars | camelCase | `configPath`, `sessionID` |

### Error Handling

```go
// Good: Handle errors explicitly
result, err := ProcessMessage(input)
if err != nil {
    return fmt.Errorf("failed to process: %w", err)
}

// Bad: Ignoring errors
result, _ := ProcessMessage(input)
```

### Context Usage

Always pass `context.Context` as the first parameter for long-running operations:

```go
func ProcessMessage(ctx context.Context, input string) (*Result, error) {
    select {
    case <-ctx.Done():
        return nil, ctx.Err()
    default:
        // proceed with operation
    }
}
```

### Documentation

Document all exported types, functions, and packages:

```go
// Package cortex provides the core cognitive architecture for the agent.
// It implements a three-layer system: Perception, Cognition, and Execution.
package cortex

// Manager handles the Cortex cognitive processing pipeline.
type Manager struct {
    // config holds the manager configuration
    config *Config
}

// ProcessMessage analyzes the user input and returns the cognitive analysis.
func (m *Manager) ProcessMessage(ctx context.Context, input string) (*Analysis, error) {
    // ...
}
```

## Testing

### Test Structure

```
internal/cortex/
├── cortex.go
├── cortex_test.go        // Unit tests
├── integration_test.go   // Integration tests
├── benchmark_test.go     // Benchmarks
└── example_test.go       // Examples
```

### Writing Tests

```go
func TestManager_ProcessMessage(t *testing.T) {
    mgr := NewManager(t.TempDir())
    defer mgr.Close()

    tests := []struct {
        name    string
        input   string
        wantErr bool
    }{
        {
            name:    "simple task",
            input:   "Write a hello world program",
            wantErr: false,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result, err := mgr.ProcessMessage(context.Background(), tt.input)
            if (err != nil) != tt.wantErr {
                t.Errorf("ProcessMessage() error = %v, wantErr %v", err, tt.wantErr)
                return
            }
            // Additional assertions
        })
    }
}
```

### Running Tests

```bash
# Run all tests
go test ./...

# Run with verbose output
go test -v ./...

# Run with coverage
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out

# Run benchmarks
go test -bench=. -benchmem ./...

# Run specific package
go test ./internal/cortex/... -v
```

## Submission Process

### 1. Create a Feature Branch

```bash
git checkout -b feature/your-feature-name
```

### 2. Make Changes

Make your changes and ensure:
- Code follows the style guide
- Tests pass
- Documentation updated if needed

### 3. Commit Changes

```bash
# Stage changes
git add .

# Commit with descriptive message
git commit -m "feat: add new feature description"
```

#### Commit Message Format

```
<type>(<scope>): <description>

[optional body]

[optional footer]
```

Types:
- `feat`: New feature
- `fix`: Bug fix
- `docs`: Documentation changes
- `style`: Code style changes
- `refactor`: Code refactoring
- `test`: Adding tests
- `chore`: Maintenance tasks

### 4. Push and Create PR

```bash
git push origin feature/your-feature-name
```

Then create a Pull Request on GitHub.

### 5. PR Requirements

- Pass all CI checks
- Include tests for new functionality
- Update documentation if needed
- Reference related issues

## CI/CD

### GitHub Actions

The project uses GitHub Actions for CI/CD. Workflows are defined in `.github/workflows/`.

### Checks Performed

1. **Lint**: Go fmt, go vet, static analysis
2. **Test**: Unit and integration tests
3. **Coverage**: Ensure adequate test coverage
4. **Build**: Verify project builds successfully

### Local CI Simulation

```bash
# Run all checks locally
make ci

# Or individually
make fmt
make vet
make test
make build
```

## Project Structure

```
go-magic/
├── cmd/magic/           # CLI entry point
├── internal/            # Private packages
│   ├── cortex/          # Core cognitive architecture
│   ├── agent/           # Agent implementation
│   ├── cognition/       # Cognition layer
│   ├── execution/       # Execution layer
│   ├── memory/          # Memory system
│   ├── perception/      # Perception layer
│   ├── plugin/          # Plugin system
│   ├── tool/            # Tool system
│   └── ...
├── pkg/                 # Public packages
├── docs/                # Documentation
├── examples/            # Example code
└── Makefile            # Build automation
```
