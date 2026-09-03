---
name: code-review
description: "Code review best practices and guidelines for effective peer reviews"
version: 1.0.0
author: go-magic
tags: [code-review, quality, collaboration, development]
tools: [read_file, file_search]
---

# Code Review Skill

## When to Use

Load this skill when:
- Reviewing pull requests or merge requests
- Establishing code review standards for a team
- Creating code review checklists
- Training new team members on review practices

## Quick Reference

### Review Checklist
```
✓ Security vulnerabilities checked
✓ Error handling comprehensive
✓ Code follows project conventions
✓ Performance implications considered
✓ Test coverage adequate
✓ Documentation complete
```

## What to Check

### Security
- [ ] Input validation and sanitization
- [ ] SQL injection prevention
- [ ] XSS prevention
- [ ] No hardcoded secrets
- [ ] Authentication/authorization checks

### Code Quality
- [ ] Clear, descriptive naming
- [ ] Single responsibility principle
- [ ] No duplicated code
- [ ] Proper abstraction levels
- [ ] Follows project conventions

### Error Handling
- [ ] All errors are caught
- [ ] Error messages are helpful
- [ ] Edge cases handled
- [ ] No silent failures

### Performance
- [ ] No N+1 queries
- [ ] Appropriate caching
- [ ] Database queries optimized
- [ ] Pagination for large data

## Comment Format

```
[Priority] Type: Description

Example:
[High] Security: SQL injection vulnerability in user input
[Medium] Performance: N+1 query in user listing
[Low] Style: Consider extracting to helper function
```

## Best Practices

### For Reviewers
- ✅ Review promptly (within 24-48 hours)
- ✅ Ask questions instead of demanding
- ✅ Praise good code
- ✅ Be objective, not personal
- ✅ Suggest, don't command

### For Authors
- ✅ Keep PRs focused (< 500 lines)
- ✅ Self-review before requesting
- ✅ Respond to all comments
- ✅ Fix issues, don't defend
