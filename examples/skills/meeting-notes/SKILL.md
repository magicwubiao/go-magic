---
name: meeting-notes
description: "Use when taking, structuring, or distributing meeting minutes — capturing decisions, action items with owners and deadlines, and turning raw notes or a transcript into a clean summary."
version: 1.1.0
author: go-magic
license: MIT
tags: [meeting, minutes, notes, productivity, office]
tools: [read_file, write_file, file_edit]
---

# Meeting Notes Guide

How to capture what matters in a meeting and produce minutes people
actually read.

## When to Use

Load this skill when:
- Taking live notes during a meeting
- Converting raw notes or a transcript into structured minutes
- Distributing a summary after the meeting
- Tracking action items from previous meetings

## Quick Reference

### Minute structure
```
Meeting   title, date/time, attendees
Purpose   one line: why this meeting existed
Decisions what was decided (the core of the minutes)
Actions   owner + task + deadline, one row each
Open      unresolved questions / parked topics
Next      date of the follow-up meeting
```

### Action item format
```
[Owner] Task — due YYYY-MM-DD
例: @Alice Send revised pricing to vendor — due 2026-11-10
```

## The rules

1. **Decisions over discussion.** Record what was decided and by whom;
   the debate itself is history.
2. **Every action item has an owner and a date.** An item without both
   will not happen.
3. **Attribute disagreements neutrally.** "Option B was deferred pending
   cost data" — not "Alice blocked it".
4. **Send within 24 hours.** Later than that, the actions are already
   stale.
5. **Keep it scannable.** Bullets and tables, no paragraphs.

## Templates

### Post-meeting minutes
```markdown
# <Meeting Name> Minutes

- **Date:** 2026-11-05 14:00–15:00
- **Attendees:** Alice, Bob, Carol
- **Apologies:** Dave

## Purpose
Decide the Q4 launch date and owners.

## Decisions
1. Launch moved to Dec 1 (risk buffer for the payment migration).
2. Marketing owns the press kit; budget capped at ¥50,000.

## Action Items
| Owner | Task | Due |
| ----- | ---- | --- |
| Alice | Revised launch checklist | 2026-11-08 |
| Bob   | Confirm vendor pricing  | 2026-11-10 |
| Carol | Draft press timeline    | 2026-11-12 |

## Open Questions
- Whether the mobile build ships with v1 — revisit next week.

## Next Meeting
2026-11-12 14:00
```

### From transcript to minutes
1. Skim the whole transcript for decisions first ("we'll go with",
   "agreed", "so the plan is").
2. Collect every commitment ("I'll", "can you send") into the action
   table — verify the owner for each.
3. Anything discussed but unresolved goes under Open Questions.
4. Cut small talk, rehash, and anything already minuted before.

## Follow-up email skeleton

```text
Subject: Minutes: <meeting name> — 3 action items

Hi all,

Minutes below. Please correct anything I misrecorded by EOD tomorrow.

Decisions: <one line each>
Actions: <owner — task — due>

Next meeting: <date>
```

## Pitfalls

- Do not record sensitive compensation/personnel details in minutes
  that circulate widely.
- Verbatim transcripts are not minutes — summarize.
- If a decision was made without consensus, note that fact rather than
  presenting it as unanimous.
- Keep old action items visible in follow-ups until they are done,
  not just the new ones.

## Delivery checklist

- [ ] Every decision has context the reader can understand
- [ ] Every action item has owner + date
- [ ] Attendee list complete; absentees noted
- [ ] Sent within 24 hours to all attendees
