---
name: git-workflow
description: "Git workflow best practices and guidelines for efficient branch management, commits, and collaboration."
version: 1.0.0
author: magic
license: MIT
metadata:
  hermes:
    tags: [git, workflow, collaboration, version-control]
    category: software-development
---

# Git Workflow Guide

A comprehensive guide for Git branch management, commit conventions, and collaborative development workflows.

## When to Use

Load this skill when:
- Setting up a new repository or branch strategy
- Creating feature branches or bugfix branches
- Writing commit messages
- Opening or reviewing pull requests
- Dealing with merge conflicts
- Need to rebase, squash, or amend commits

## Quick Reference

```bash
# Branch naming conventions
git checkout -b feat/add-login           # New feature
git checkout -b fix/auth-bug              # Bug fix
git checkout -b docs/update-readme         # Documentation
git checkout -b refactor/api-cleanup       # Code refactoring

# Conventional commits
git commit -m "feat: add user login endpoint"
git commit -m "fix: resolve authentication timeout"
git commit -m "docs: update API documentation"

# Keep branch updated
git fetch origin
git rebase origin/main

# PR workflow
git push origin feat/add-login
# Then create PR via GitHub/GitLab/Bitbucket UI
```

## Branch Naming Convention

Follow the format: `type/description`

| Type | Usage |
|------|-------|
| `feat/` | New features |
| `fix/` | Bug fixes |
| `docs/` | Documentation changes |
| `refactor/` | Code refactoring |
| `test/` | Adding or updating tests |
| `chore/` | Maintenance tasks |
| `perf/` | Performance improvements |

Rules:
- Use lowercase with hyphens: `feat/add-user-auth`
- Keep descriptions concise but descriptive
- No spaces or special characters

## Commit Message Format

Follow [Conventional Commits](https://www.conventionalcommits.org/):

```
<type>(<scope>): <subject>

<body>

<footer>
```

### Examples

```bash
# Simple commit
git commit -m "feat: add password reset endpoint"

# With scope
git commit -m "fix(auth): resolve token expiration issue"

# With body
git commit -m "feat: implement user profile page

- Add profile display component
- Add edit profile functionality
- Integrate with user API"

# Breaking change
git commit -m "feat: redesign API endpoints

BREAKING CHANGE: API endpoints now require authentication"
```

## PR Process

1. **Create PR early** - Use draft PRs to indicate work-in-progress
2. **Fill PR template** - Provide context, testing steps, screenshots
3. **Request reviews** - Tag appropriate team members
4. **Address feedback** - Respond to comments, make requested changes
5. **Squash or rebase** - Clean up commits before merging
6. **Delete branch** - Clean up after merge

### PR Title Format
```
feat: Add user authentication system
fix: Resolve login redirect loop
docs: Update deployment guide
```

## Best Practices

### Do's
- ✅ Write atomic commits (one logical change per commit)
- ✅ Write meaningful commit messages
- ✅ Keep branches focused on a single task
- ✅ Rebase before merging when appropriate
- ✅ Run tests before pushing
- ✅ Use `.gitignore` properly

### Don'ts
- ❌ Commit directly to `main`/`master`
- ❌ Mix unrelated changes in one commit
- ❌ Force push to shared branches
- ❌ Leave large commented-out code blocks
- ❌ Commit sensitive data (API keys, passwords)

## Common Workflows

### Feature Development
```bash
# 1. Start from main
git checkout main
git pull origin main

# 2. Create feature branch
git checkout -b feat/new-feature

# 3. Develop and commit
git add .
git commit -m "feat: implement new feature"

# 4. Keep updated with main
git fetch origin
git rebase origin/main

# 5. Push and create PR
git push origin feat/new-feature
```

### Hotfix
```bash
# 1. Start from main
git checkout main
git pull origin main

# 2. Create hotfix branch
git checkout -b fix/critical-bug

# 3. Fix and commit
git add .
git commit -m "fix: resolve critical production bug"

# 4. Push and create PR (mark as urgent)
git push origin fix/critical-bug
```

## Pitfalls

### Merge Conflicts
**Problem**: Conflicts when merging/rebasing  
**Solution**: 
- Pull/rebase frequently to minimize conflicts
- Communicate with team about overlapping changes
- Use `git status` to identify conflicted files
- Edit files to resolve conflicts, then `git add` and `git commit`

### Forgetting to Pull
**Problem**: Local branch diverges significantly from remote  
**Solution**: 
- Always `git pull --rebase` before starting new work
- Set up git alias: `git config --global alias.up 'pull --rebase'`

### Dangling Commits
**Problem**: Commits on detached HEAD or abandoned branches  
**Solution**: 
- Always create a branch for new work
- Use `git reflog` to recover lost commits

## Verification

After setting up workflow:
1. Verify branch naming: `git branch` shows proper names
2. Check commit format: `git log --oneline` shows conventional commits
3. Confirm PR template: Create test PR to verify template appears
4. Test rebase: `git rebase main` completes without conflicts

## Tools & References

- [Conventional Commits](https://www.conventionalcommits.org/)
- [Git Flow](https://nvie.com/posts/a-successful-git-branching-model/)
- [GitHub Flow](https://guides.github.com/introduction/flow/)
- [Git Documentation](https://git-scm.com/doc)
