---
name: daily-report
description: "Generate structured daily reports and summaries"
version: 1.0.0
author: go-magic
tags: [report, daily, summary, productivity]
tools: [todo]
---

# Daily Report Skill

## When to Use

Load this skill when:
- User asks for a daily report
- Summarizing work completed
- Tracking progress and blockers
- Preparing standup updates

## Report Templates

### Daily Standup Report
```
## Daily Standup - [Date]

**Completed Yesterday**:
- [ ] Task 1
- [ ] Task 2

**Working On Today**:
- [ ] Task 1
- [ ] Task 2

**Blockers**:
- None / [Description]
```

### Daily Progress Report
```
## Daily Progress Report - [Date]

### Accomplishments
1. **Project/Task**: Description of work done
2. **Project/Task**: Description of work done

### In Progress
- Task 1 (X% complete)
- Task 2 (Y% complete)

### Challenges
- Challenge 1
- Challenge 2

### Tomorrow's Plan
- [ ] Task 1
- [ ] Task 2

### Metrics
- Tasks completed: X
- Hours worked: Y
- Meetings attended: Z
```

### Weekly Summary
```
## Weekly Summary - [Week of Date]

### High Priority
1. Item 1
2. Item 2

### Accomplished
| Task | Status | Notes |
|------|--------|-------|
| Task 1 | ✅ Done | Completed on time |
| Task 2 | ✅ Done | Completed on time |
| Task 3 | 🔄 Partial | 80% done |

### Next Week's Goals
- [ ] Goal 1
- [ ] Goal 2

### Key Metrics
- Velocity: X points
- Bugs fixed: Y
- PRs merged: Z
```

## Report Generation Tips

### Be Specific
- Include concrete deliverables
- Mention specific outcomes
- Reference ticket/PR numbers

### Show Progress
- Use percentages when applicable
- Compare to goals
- Highlight achievements

### Be Honest About Blockers
- Identify issues early
- Request help proactively
- Document challenges

### Keep It Concise
- Stick to key points
- Use bullet points
- Avoid unnecessary details

## Metrics to Track

- Tasks completed
- Hours worked
- Meetings attended
- Bugs found/fixed
- PRs merged
- Goals achieved

## Example Report

```
## Daily Report - 2024-01-15

### Today
✅ Completed user authentication refactor (PR #123)
✅ Fixed critical bug in payment flow
✅ Reviewed 3 pull requests

### Progress
- Feature X: 75% complete
- Feature Y: 40% complete

### Tomorrow
- [ ] Complete Feature X
- [ ] Start Feature Y integration
- [ ] Team code review

### Blockers
- Waiting on API documentation from team
```

## Integration with Todo

Use `todo` tool to:
- List current tasks
- Mark completed items
- Track priorities
- Generate reports from todo data
