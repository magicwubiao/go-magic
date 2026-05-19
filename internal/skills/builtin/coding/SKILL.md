---
name: coding
description: "Advanced coding assistance with code review, optimization, design patterns, and refactoring guidance"
version: 1.0.0
author: go-magic
tags: [coding, review, optimization, design-patterns, refactoring]
tools: [read_file, file_search, lint, format_code]
---

# Coding Skill

## When to Use

Load this skill when:
- Writing or reviewing code
- Optimizing code performance
- Applying design patterns
- Refactoring existing code
- Seeking best practices guidance
- Improving code quality

## Capabilities

### 1. Code Review

Comprehensive code review covering:
- **Correctness**: Logic errors, edge cases, potential bugs
- **Performance**: Algorithm efficiency, resource usage, bottlenecks
- **Security**: Input validation, injection risks, secret handling
- **Maintainability**: Code clarity, documentation, test coverage
- **Style**: Consistency with project conventions

### 2. Code Optimization

Optimization strategies for:
- **Time Complexity**: Algorithm improvements, caching strategies
- **Space Complexity**: Memory usage reduction, data structure selection
- **I/O Optimization**: Batch processing, async operations
- **Concurrency**: Parallel processing, goroutines, channels
- **Database**: Query optimization, indexing, N+1 prevention

### 3. Design Patterns

Common patterns and when to use them:

#### Creational Patterns
- **Singleton**: Single instance management
- **Factory**: Object creation abstraction
- **Builder**: Complex object construction
- **Prototype**: Object cloning

#### Structural Patterns
- **Adapter**: Interface compatibility
- **Decorator**: Behavior extension
- **Facade**: Simplified interface
- **Proxy**: Access control, lazy loading

#### Behavioral Patterns
- **Observer**: Event subscription
- **Strategy**: Algorithm interchangeability
- **Command**: Request encapsulation
- **Chain of Responsibility**: Request handling chain

### 4. Refactoring Guidance

Refactoring techniques:
- **Extract Method**: Break down large functions
- **Inline Method**: Remove unnecessary indirection
- **Move Method**: Relocate methods to appropriate classes
- **Rename**: Improve naming clarity
- **Replace Conditional with Polymorphism**: Simplify complex conditionals

## Review Checklist

### Security Checklist
```
[ ] Input validation and sanitization
[ ] SQL injection prevention (use parameterized queries)
[ ] XSS prevention (escape output)
[ ] No hardcoded secrets or credentials
[ ] Proper authentication/authorization
[ ] Secure random number generation
[ ] HTTPS for sensitive data transmission
[ ] Proper error handling (no information leakage)
```

### Performance Checklist
```
[ ] No N+1 query problems
[ ] Appropriate caching implemented
[ ] Database queries optimized
[ ] Pagination for large datasets
[ ] Resource cleanup (files, connections)
[ ] Memory allocation optimized
[ ] Concurrency used where appropriate
```

### Code Quality Checklist
```
[ ] Clear, descriptive naming
[ ] Single responsibility principle
[ ] No duplicated code (DRY)
[ ] Proper abstraction levels
[ ] Comprehensive error handling
[ ] Unit tests for critical paths
[ ] Documentation for public APIs
[ ] Consistent code style
```

## Comment Format

```
[Priority] [Category]: Description

Examples:
[High] Security: SQL injection vulnerability in user input
[High] Performance: N+1 query in user listing
[Medium] Style: Consider extracting to helper function
[Low] Documentation: Missing function documentation
```

## Best Practices by Language

### Go
- Use `gofmt` for formatting
- Follow Effective Go guidelines
- Handle errors explicitly
- Use interfaces for abstraction
- Prefer composition over inheritance
- Use channels for goroutine communication

### JavaScript/TypeScript
- Use ESLint and Prettier
- Prefer `const` and `let` over `var`
- Use async/await for asynchronous code
- Handle promise rejections
- Use TypeScript for type safety
- Follow module best practices

### Python
- Follow PEP 8 style guide
- Use type hints (Python 3.5+)
- Use list/dict comprehensions appropriately
- Handle exceptions specifically
- Use context managers (with statements)
- Document with docstrings

### Rust
- Follow Rust API guidelines
- Use `cargo clippy` for linting
- Leverage ownership and borrowing
- Use pattern matching effectively
- Handle errors with Result/Option
- Write unsafe code only when necessary

## Optimization Guidelines

### Algorithm Optimization
1. **Analyze complexity**: Determine Big-O of current solution
2. **Identify bottlenecks**: Profile to find slow sections
3. **Choose appropriate data structures**: Hash maps for lookups, trees for ordering
4. **Consider trade-offs**: Time vs space, readability vs performance

### Database Optimization
1. **Add indexes**: For frequently queried columns
2. **Optimize queries**: Select only needed columns
3. **Use batch operations**: Reduce round trips
4. **Implement caching**: Redis, in-memory, or query cache
5. **Connection pooling**: Reuse database connections

### Concurrency Optimization
1. **Identify parallelizable work**: Independent computations
2. **Use appropriate primitives**: Mutex, RWMutex, channels
3. **Avoid premature optimization**: Profile first
4. **Prevent race conditions**: Use proper synchronization
5. **Limit goroutine/thread count**: Prevent resource exhaustion

## Refactoring Process

### Before Refactoring
1. Ensure tests exist and pass
2. Understand the code thoroughly
3. Identify the goal of refactoring
4. Plan incremental changes

### During Refactoring
1. Make small, focused changes
2. Run tests after each change
3. Use version control for rollback
4. Update documentation as needed

### After Refactoring
1. Verify all tests pass
2. Review for new issues
3. Update related documentation
4. Consider performance impact

## Anti-Patterns to Avoid

### Code Smells
- **God Object**: Classes with too many responsibilities
- **Feature Envy**: Method that uses more features of another class
- **Primitive Obsession**: Overuse of primitive types
- **Switch Statements**: Repeated switch/case blocks
- **Temporary Field**: Fields only used in specific circumstances

### Performance Anti-Patterns
- **Premature Optimization**: Optimizing without profiling
- **Excessive Memory Allocation**: Creating unnecessary objects
- **Blocking I/O**: Synchronous operations in async contexts
- **Memory Leaks**: Unreleased resources
- **N+1 Queries**: Querying in loops

## Example Reviews

### Example 1: Security Issue
```go
// Bad
query := "SELECT * FROM users WHERE id = " + userID

// Good
query := "SELECT * FROM users WHERE id = ?"
rows, err := db.Query(query, userID)
```

### Example 2: Performance Issue
```go
// Bad - N+1 query
for _, user := range users {
    orders := getOrders(user.ID) // Query in loop
}

// Good - Single query with JOIN
orders := getOrdersForUsers(userIDs)
```

### Example 3: Refactoring
```go
// Before
func process(data []Data) {
    for i := 0; i < len(data); i++ {
        if data[i].Type == "A" {
            processA(data[i])
        } else if data[i].Type == "B" {
            processB(data[i])
        }
    }
}

// After
func process(data []Data) {
    for _, d := range data {
        processByType(d)
    }
}

func processByType(d Data) {
    switch d.Type {
    case "A":
        processA(d)
    case "B":
        processB(d)
    }
}
```

## Tools Integration

This skill integrates with:
- `lint`: Automatic code linting
- `format_code`: Code formatting
- `read_file`: Code analysis
- `file_search`: Finding related code
