---
name: code-review
description: "Code review best practices, guidelines, and checklists for effective peer reviews."
version: 1.0.0
author: magic
license: MIT
metadata:
  hermes:
    tags: [code-review, quality, collaboration, guidelines]
    category: software-development
---

# Code Review Guide

Comprehensive guidelines for conducting effective code reviews that improve code quality, share knowledge, and catch issues early.

## When to Use

Load this skill when:
- Reviewing pull requests or merge requests
- Establishing code review standards for a team
- Creating code review checklists
- Training new team members on review practices
- Dealing with contentious review feedback

## Quick Reference

### Review Checklist
```
✓ Security vulnerabilities checked
✓ Error handling comprehensive
✓ Code follows project conventions
✓ Performance implications considered
✓ Test coverage adequate
✓ Documentation complete
✓ No sensitive data exposed
✓ Breaking changes documented
```

### Review Comments Format
```
[Priority] Type: Description

Example:
[High] Security: SQL injection vulnerability in user input handling
[Medium] Performance: N+1 query issue in user listing endpoint
[Low] Style: Consider extracting repeated logic into helper function
```

## Review Process

### 1. Understand the Context
- Read the PR description and linked issues
- Understand the motivation behind the changes
- Check if tests are included
- Review the diff stat (number of files, lines changed)

### 2. Line-by-Line Review
- Start from high-level structure to details
- Focus on logic, not just style
- Verify edge cases are handled
- Check for proper error handling

### 3. Provide Constructive Feedback
- Be specific and actionable
- Explain "why" not just "what"
- Suggest alternatives when rejecting approaches
- Use "we" instead of "you" for team ownership

## What to Check

### Security
- [ ] Input validation and sanitization
- [ ] Authentication and authorization checks
- [ ] SQL injection prevention (parameterized queries)
- [ ] XSS prevention (output encoding)
- [ ] Sensitive data handling (no hardcoded secrets)
- [ ] Dependency vulnerabilities (check `npm audit`, `pip check`, etc.)

### Code Quality
- [ ] Clear, descriptive naming
- [ ] Functions/methods have single responsibility
- [ ] No duplicated code
- [ ] Proper abstraction levels
- [ ] Follows project coding conventions
- [ ] No magic numbers or strings

### Error Handling
- [ ] All errors are properly caught
- [ ] Error messages are user-friendly
- [ ] Logging is appropriate (not too much/little)
- [ ] Edge cases are handled
- [ ] No silent failures

### Performance
- [ ] No N+1 query problems
- [ ] Appropriate use of caching
- [ ] Database queries are optimized
- [ ] Large payloads are paginated
- [ ] No unnecessary computations in loops

### Testing
- [ ] New code has corresponding tests
- [ ] Tests cover happy path and edge cases
- [ ] Tests are readable and maintainable
- [ ] No tests skipped without justification
- [ ] Integration tests for critical paths

### Documentation
- [ ] Public APIs are documented
- [ ] Complex logic has inline comments
- [ ] README updated if needed
- [ ] Breaking changes documented
- [ ] Migration guides provided if needed

## Comment Templates

### Security Issue
```
🚨 Security Concern

I noticed that user input in `[function/file]` is not properly validated. 
This could lead to [specific vulnerability, e.g., SQL injection/XSS].

**Suggestion**: Use [specific solution, e.g., parameterized queries/input validation] 
to sanitize the input.

**Example**:
```[language]
// Instead of:
query = "SELECT * FROM users WHERE id = " + userInput

// Use:
query = "SELECT * FROM users WHERE id = ?"
db.Query(query, userInput)
```
```

### Performance Issue
```
⚡ Performance Note

The current implementation fetches all records and then filters in memory 
(lines X-Y). With large datasets, this could cause memory issues.

**Suggestion**: Move the filter to the database query level.

**Example**:
```[language]
// Instead of:
users = getAllUsers()
filtered = filter(users, condition)

// Use:
filtered = getUsersWhere(condition)
```
```

### Code Quality
```
💡 Code Quality

Consider extracting the repeated logic in lines X-Y into a reusable helper 
function. This would improve maintainability and reduce duplication.

**Suggestion**:
```[language]
function processUserData(user) {
  // extracted common logic here
}
```
```

## Best Practices

### For Reviewers
- ✅ Review promptly (within 24-48 hours)
- ✅ Ask questions instead of making demands
- ✅ Praise good code ("Nice solution!", "Clean approach")
- ✅ Be objective, not personal
- ✅ Suggest, don't command
- ✅ Approve early for good code (don't delay for minor issues)

### For Authors
- ✅ Keep PRs focused and reasonably sized (< 500 lines ideally)
- ✅ Self-review before requesting reviews
- ✅ Respond to all comments
- ✅ Say "thank you" for feedback
- ✅ Fix issues, don't just defend
- ✅ Update PR description if scope changes

## Pitfalls

### Bikeshedding
**Problem**: Focusing on trivial style issues instead of important logic  
**Solution**: 
- Use linters/formatters to automate style checks
- Prioritize review comments by impact (security > logic > style)
- Set clear team conventions to avoid debates

### Review Fatigue
**Problem**: Large PRs are overwhelming to review  
**Solution**: 
- Keep PRs small (< 500 lines)
- Break large features into smaller, reviewable chunks
- Use "Draft" PRs for early feedback on approach

### Hostile Reviews
**Problem**: Review comments feel personal or aggressive  
**Solution**: 
- Use "we" language instead of "you"
- Focus on the code, not the person
- Assume positive intent
- If tensions rise, switch to video call or in-person discussion

## Verification

After completing a review:
1. All checklist items addressed
2. Feedback is constructive and actionable
3. No blocking issues remain
4. Author understands suggested changes
5. Tests pass and coverage is adequate

## Tools

- **Linters**: ESLint, Pylint, golangci-lint (automate style checks)
- **Security**: SonarQube, Snyk, OWASP ZAP
- **Coverage**: Jest, pytest-cov, go test -cover
- **PR Templates**: Set up in GitHub/GitLab/Bitbucket

## References

- [Google's Code Review Developer Guide](https://google.github.io/eng-practices/review/)
- [Best Practices for Code Review by SmartBear](https://smartbear.com/learn/code-review/best-practices-for-peer-code-review/)
- [Effective Code Reviews Without the Pain](https://www.developer.com/design-development/effective-code-reviews/)
