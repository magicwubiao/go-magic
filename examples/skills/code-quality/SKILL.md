---
name: code-quality
description: "Use when establishing code quality standards or linting and formatting for a project, when measuring quality metrics such as coverage and complexity, or when refactoring legacy code toward a consistent, maintainable standard."
version: 1.1.0
author: go-magic
license: MIT
tags: [code-quality, standards, clean-code, best-practices]
tools: [execute_command, read_file, lint, format_code, search_in_files]
---

# Code Quality Guide

Standards, metrics, and practices for keeping a codebase clean and maintainable.

## When to Use

Load this skill when:
- Establishing quality standards for a project
- Setting up linters, formatters, and pre-commit hooks
- Reviewing or refactoring code toward a consistent standard
- Measuring coverage and complexity
- Onboarding someone to the project's quality bar

## Quick Reference

### Quality Checklist
```
✓ Clear, descriptive naming
✓ One responsibility per function
✓ No duplicated code
✓ Errors handled, not swallowed
✓ Adequate test coverage
✓ No magic numbers or strings
✓ Comments explain "why", not "what"
✓ Consistent formatting (enforced by a tool, not by reviewers)
```

### Common Tools
| Language | Linter | Formatter |
|----------|--------|-----------|
| Go | `go vet`, `golangci-lint`, `staticcheck` | `gofmt` / `goimports` |
| Python | `ruff`, `pylint`, `flake8` | `black`, `ruff format` |
| JavaScript / TypeScript | `eslint` | `prettier` |
| Java | `checkstyle`, `spotbugs` | `google-java-format` |
| Rust | `clippy` | `rustfmt` |

## Principles

### 1. Readability
Code is read far more often than it is written.

```go
// Bad: names carry no meaning, and the resource is never closed
func p(u string) error {
	r, e := h.g(u)
	if e != nil {
		return e
	}
	return r.s()
}

// Good: intent is explicit and the response body is released
func fetchAndDrain(url string) error {
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("failed to fetch %s: %w", url, err)
	}
	defer resp.Body.Close()

	_, err = io.Copy(io.Discard, resp.Body)
	return err
}
```

### 2. Single Responsibility
One function, one reason to change.

```python
# Bad: validate + persist + notify + log in one function
def process_user_data(user):
    if not user.email:
        raise ValueError("Email required")
    db.save(user)
    send_welcome_email(user)
    logger.info(f"User {user.id} processed")

# Good: each step is separable and testable
def validate_user(user):
    if not user.email:
        raise ValueError("Email required")

def save_user(user):
    db.save(user)

def notify_user(user):
    send_welcome_email(user)

def process_user(user):
    validate_user(user)
    save_user(user)
    notify_user(user)
    logger.info(f"User {user.id} processed")
```

### 3. DRY — without over-abstracting
```javascript
// Bad: the same loop written twice
function calculateTotal(items) {
  let total = 0;
  for (const item of items) total += item.price * item.quantity;
  return total;
}
function calculateTax(items) {
  let total = 0;
  for (const item of items) total += item.price * item.quantity;
  return total * 0.1;
}

// Good: one definition of "subtotal"
const calculateSubtotal = (items) =>
  items.reduce((sum, item) => sum + item.price * item.quantity, 0);

const calculateTotal = (items) => calculateSubtotal(items);
const calculateTax = (items) => calculateSubtotal(items) * 0.1;
```

Two similar lines are not duplication. Extract when the *reason to change* is shared, not when the text merely matches.

## Quality Metrics

### Test coverage
| Coverage | Rating | Action |
|----------|--------|--------|
| 90%+ | Excellent | Maintain |
| 80-90% | Good | Target this |
| 70-80% | Acceptable | Improve the risky paths first |
| <70% | Poor | Needs work |

Coverage is a floor, not a goal — 100% coverage of trivial getters says nothing.

### Complexity
| Metric | Good | Warning | Bad |
|--------|------|---------|-----|
| Cyclomatic complexity | <10 | 10-15 | >15 |
| Function length | <20 lines | 20-50 | >50 |
| File length | <300 lines | 300-500 | >500 |
| Nesting depth | <3 | 3-4 | >4 |

### Maintainability index
```
0-100, higher is better:  >85 good   ·   65-85 moderate   ·   <65 poor
```

## Tool Setup

### Go
```bash
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

gofmt -l .            # list files that are not formatted
golangci-lint run     # lint
go vet ./...          # standard checks
go test -race ./...   # tests under the race detector
```
Minimal `.golangci.yml`:
```yaml
linters:
  enable:
    - govet
    - staticcheck
    - errcheck
```

### Python
```bash
pip install ruff black
ruff check .      # lint
black .           # format
```

### JavaScript / TypeScript
```bash
npm i -D eslint prettier
npx eslint --fix src/
npx prettier --write src/
```

### Pre-commit hook
Run the formatter and linter on staged files so style never reaches review:
```bash
#!/bin/sh
# .git/hooks/pre-commit
gofmt -l $(git diff --name-only --cached | grep '\.go$') | grep . && exit 1
```
(Prefer a managed hook tool — `pre-commit`, `lefthook`, `husky` — over hand-editing `.git/hooks`.)

## Best Practices

### Naming
- ✅ Descriptive: `calculateTotalPrice()` not `calc()`
- ✅ Booleans read as predicates: `isValid`, `hasPermission`, `canRetry`
- ✅ Types/classes: `PascalCase`
- ⚠️ Constants: follow the language — Go uses `PascalCase` (`MaxRetries`) or `camelCase`; Python/Java/C use `UPPER_SNAKE_CASE` (`MAX_RETRIES`). `UPPER_SNAKE_CASE` in Go is not idiomatic.
- ✅ Short names are fine for short scopes: `i`, `err`, `ctx`

### Error handling
```python
# Bad: swallows everything
def process(data):
    try:
        return transform(data)
    except:
        pass

# Good: handle what you can name, preserve the rest
def process(data):
    try:
        return transform(data)
    except ValidationError as e:
        logger.warning("Validation failed: %s", e)
        raise
    except Exception as e:
        logger.error("Unexpected error: %s", e)
        raise InternalServerError("Processing failed") from e
```

### Comments
```go
// Bad: restates the code
// This function adds two numbers
func add(a, b int) int { return a + b }

// Good: explains a decision the reader cannot infer
// 1024 is a multiple of the 512-byte sector size, so writes stay aligned.
const writeBufferSize = 1024
```

## Pitfalls

### Over-engineering
**Problem**: abstraction built for requirements that do not exist.
**Solution**: YAGNI — add the abstraction when the second real case appears.

### Ignoring linter warnings
**Problem**: warnings accumulate until the signal is lost.
**Solution**: treat warnings as errors in CI.

### Inconsistent style
**Problem**: mixed formatting makes diffs noisy and review harder.
**Solution**: one auto-formatter, run in pre-commit and CI; never argue style in review.

### Low coverage on risky code
**Problem**: the paths most likely to break are the untested ones.
**Solution**: set a coverage floor in CI and review coverage per change, not just in aggregate.

## Verification

After setting up quality tooling:
1. Linter runs clean: `golangci-lint run` / `ruff check .` / `eslint src/`
2. Formatter is applied: `gofmt -l .` prints nothing
3. Coverage measured: `go test -cover ./...` / `pytest --cov`
4. Complexity checked on the changed packages
5. CI fails on any of the above

## References

- [Clean Code — Robert Martin](https://www.oreilly.com/library/view/clean-code-a/9780136083238/)
- [Google Style Guides](https://google.github.io/styleguide/)
- [OWASP Secure Coding Practices](https://owasp.org/www-project-secure-coding-practices-quick-reference-guide/)
- [Go Code Review Comments](https://go.dev/wiki/CodeReviewComments)
