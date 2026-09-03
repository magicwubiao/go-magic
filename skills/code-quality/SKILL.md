---
name: code-quality
description: "Code quality standards, metrics, and practices for maintaining clean, maintainable codebases."
version: 1.0.0
author: magic
license: MIT
metadata:
  hermes:
    tags: [code-quality, standards, clean-code, best-practices]
    category: software-development
---

# Code Quality Guide

Standards, metrics, and practices to ensure high-quality, maintainable code.

## When to Use

Load this skill when:
- Establishing code quality standards for a project
- Setting up linting and formatting tools
- Reviewing code for quality issues
- Measuring code quality metrics
- Refactoring legacy code
- Onboarding team members to quality practices

## Quick Reference

### Quality Checklist
```
✓ Clear, descriptive naming
✓ Functions have single responsibility
✓ No duplicated code
✓ Proper error handling
✓ Adequate test coverage (>80%)
✓ No magic numbers or strings
✓ Comments explain "why", not "what"
✓ Consistent style and formatting
```

### Common Tools
| Language | Linter | Formatter |
|----------|--------|----------|
| Python | pylint, flake8 | black, yapf |
| JavaScript | eslint | prettier |
| Go | golangci-lint | gofmt |
| Java | checkstyle | google-java-format |
| Rust | clippy | rustfmt |

## Code Quality Principles

### 1. Readability
Code is read more often than written. Prioritize clarity:

```go
// Bad: unclear intent
func p(u string) error {
  r, e := h.g(u)
  if e != nil { return e }
  return r.s()
}

// Good: clear intent
func processURL(url string) error {
  response, err := httpClient.Get(url)
  if err != nil {
    return fmt.Errorf("failed to fetch URL: %w", err)
  }
  defer response.Body.Close()
  return response.Body.Close()
}
```

### 2. Single Responsibility
Each function/class should do one thing well:

```python
# Bad: multiple responsibilities
def process_user_data(user):
    # Validate
    if not user.email:
        raise ValueError("Email required")
    # Save to DB
    db.save(user)
    # Send email
    send_welcome_email(user)
    # Log
    logger.info(f"User {user.id} processed")

# Good: separated responsibilities
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

### 3. DRY (Don't Repeat Yourself)
Eliminate duplication:

```javascript
// Bad: duplicated logic
function calculateTotal(items) {
  let total = 0;
  for (let item of items) {
    total += item.price * item.quantity;
  }
  return total;
}

function calculateTax(items) {
  let total = 0;
  for (let item of items) {
    total += item.price * item.quantity;
  }
  return total * 0.1; // 10% tax
}

// Good: extract common logic
function calculateSubtotal(items) {
  return items.reduce((sum, item) => sum + item.price * item.quantity, 0);
}

function calculateTotal(items) {
  return calculateSubtotal(items);
}

function calculateTax(items) {
  return calculateSubtotal(items) * 0.1;
}
```

## Quality Metrics

### Test Coverage
| Coverage | Rating | Action |
|----------|--------|--------|
| 90%+ | Excellent | Maintain |
| 80-90% | Good | Aim for this |
| 70-80% | Acceptable | Improve |
| <70% | Poor | Needs work |

### Code Complexity
| Metric | Good | Warning | Bad |
|--------|------|---------|-----|
| Cyclomatic Complexity | <10 | 10-15 | >15 |
| Function Length | <20 lines | 20-50 | >50 |
| File Length | <300 lines | 300-500 | >500 |
| Nesting Depth | <3 | 3-4 | >4 |

### Maintainability Index
```
0-100 scale (higher is better):
  - >85: Good
  - 65-85: Moderate
  - <65: Bad
```

## Tool Setup

### Python (pylint + black)
```bash
# Install
pip install pylint black

# Check code
pylint mymodule.py

# Auto-format
black .

# Pre-commit hook
echo "python -m pylint \$(git diff --name-only --cached | grep .py)" > .git/hooks/pre-commit
chmod +x .git/hooks/pre-commit
```

### JavaScript/TypeScript (eslint + prettier)
```bash
# Install
npm install --save-dev eslint prettier

# Initialize eslint
npx eslint --init

# Check and fix
npx eslint --fix src/

# Format
npx prettier --write src/
```

### Go (golangci-lint)
```bash
# Install
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

# Run
golangci-lint run

# Config (.golangci.yml)
linters:
  enable:
    - govet
    - staticcheck
    - errcheck
```

## Best Practices

### Naming
- ✅ Use descriptive names: `calculateTotalPrice()` not `calc()`
- ✅ Boolean variables start with `is`, `has`, `can`: `isValid`, `hasPermission`
- ✅ Constants are UPPER_SNAKE_CASE: `MAX_RETRIES`
- ✅ Classes are PascalCase: `UserProfile`
- ✅ Functions/variables are camelCase or snake_case (consistent)

### Error Handling
```python
# Bad: silent failure
def process(data):
    try:
        return transform(data)
    except:
        pass

# Good: proper error handling
def process(data):
    try:
        return transform(data)
    except ValidationError as e:
        logger.warning(f"Validation failed: {e}")
        raise
    except Exception as e:
        logger.error(f"Unexpected error: {e}")
        raise InternalServerError("Processing failed") from e
```

### Comments
```go
// Bad: states the obvious
// This function adds two numbers
func add(a, b int) int {
    return a + b
}

// Good: explains why
// Use buffer to batch writes - improves throughput by 10x
var writeBuffer = make([]byte, 0, 1024)
```

## Pitfalls

### Over-Engineering
**Problem**: Making code too abstract/complex for current needs  
**Solution**: YAGNI (You Aren't Gonna Need It) - keep it simple

### Ignoring Warnings
**Problem**: Linter warnings accumulate, code degrades  
**Solution**: Treat warnings as errors in CI

### Inconsistent Style
**Problem**: Mixed formatting makes code hard to read  
**Solution**: Use auto-formatters (black, prettier, gofmt) in pre-commit hooks

### Low Test Coverage
**Problem**: Critical code paths untested  
**Solution**: Set coverage thresholds in CI (e.g., 80% minimum)

## Verification

After setting up quality tools:
1. Run linter: `pylint .` or `eslint src/`
2. Run formatter: `black .` or `prettier --write .`
3. Check coverage: `pytest --cov`
4. Measure complexity: `radon cc .` (Python) or `eslint --max-warnings 0`
5. CI passes with all quality gates

## Tools & References

- [Clean Code by Robert Martin](https://www.amazon.com/Clean-Code-Handbook-Software-Craftsmanship/dp/0132350882)
- [Google Style Guides](https://google.github.io/styleguide/)
- [OWASP Secure Coding Practices](https://owasp.org/www-project-secure-coding-practices-quick-reference-guide/)
- [SonarQube](https://www.sonarqube.org/) (code quality platform)
