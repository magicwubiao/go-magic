---
name: ppt-presentations
description: "Use when creating or editing PowerPoint presentations (.pptx) — structuring a deck from an outline, writing slide content, adding speaker notes, or restyling an existing deck."
version: 1.1.0
author: go-magic
license: MIT
tags: [ppt, powerpoint, slides, presentation, office]
tools: [execute_command, read_file, write_file, file_edit]
---

# Presentation Guide

How to build a deck that communicates: structure first, slides second.

## When to Use

Load this skill when:
- Turning an outline, document, or data into a .pptx deck
- Restructuring or rewriting slide content
- Adding consistent titles, bullet hierarchies, or speaker notes
- Editing an existing presentation programmatically

## Quick Reference

### Deck structure
```
1  Title slide       title, subtitle, date, presenter
2  Agenda / TOC      what the audience will learn
3+  Content          one idea per slide
n-1 Summary          repeat the key takeaways
n  Q&A / Thanks      contact info
```

### Slide density rule
One message per slide. Max 6 bullet lines, max ~12 words per line.
If a slide needs more, split it or move detail to speaker notes.

## The rules

1. **Outline before slides.** Get the storyline approved, then build.
2. **Titles carry the message.** "Sales grew 23% in Q3" beats "Q3 Sales".
3. **Consistent layout.** Same title position, font sizes, and colors on
   every content slide — define once in the slide master if possible.
4. **Speaker notes over bullets.** Detail belongs in notes, not on screen.
5. **Numbers get sources.** Any figure on a slide cites where it came from.

## Common recipes

### Generate a deck
```python
from pptx import Presentation
from pptx.util import Inches, Pt

prs = Presentation()
slide = prs.slides.add_slide(prs.slide_layouts[1])  # title + content
slide.shapes.title.text = "Sales grew 23% in Q3"
body = slide.placeholders[1].text_frame
body.text = "Enterprise segment led growth"
p = body.add_paragraph(); p.text = "Churn fell to 2.1%"
p.font.size = Pt(18)
prs.save("deck.tmp.pptx")  # atomic: rename afterwards
```

### Edit an existing deck
```python
prs = Presentation("input.pptx")
for i, slide in enumerate(prs.slides):
    for shape in slide.shapes:
        if shape.has_text_frame:
            print(i, shape.text_frame.text[:80])
```
Inspect before editing — slide layouts differ per template, and
placeholder indexes are not stable across decks.

## Content patterns

- **Data slide:** chart > table > text. Label the takeaway on the slide,
  keep the raw numbers in the appendix.
- **Comparison slide:** two columns, criteria as rows, verdict in title.
- **Process slide:** numbered steps, one line each; details in notes.

## Pitfalls

- Charts: python-pptx can add native charts, but complex styling is
  easier done by inserting a pre-rendered image.
- Fonts must exist on the presentation machine; check before delivery.
- Do not exceed the placeholder box — overflowing text is invisible in
  show mode but looks broken in editing.
- When editing a user's deck, keep the original theme/master untouched.

## Delivery checklist

- [ ] Storyline: title slide answers "why should I care?"
- [ ] Every slide has a message-carrying title
- [ ] No orphan slides or empty placeholders
- [ ] Speaker notes present for content slides
- [ ] File opens cleanly (temp + rename save)
