---
name: word-documents
description: "Use when creating or editing Word documents (.docx) — reports, contracts, official letters, resumes, or applying structured formatting with headings, tables, and styles."
version: 1.1.0
author: go-magic
license: MIT
tags: [word, docx, documents, office, writing]
tools: [execute_command, read_file, write_file, file_edit]
---

# Word Document Guide

How to produce and edit .docx files with clean, consistent structure.

## When to Use

Load this skill when:
- Generating a report, proposal, contract, or official document
- Converting notes/Markdown into a formatted Word document
- Editing an existing .docx (replace sections, update tables)
- Applying consistent heading, list, and table styles

## Quick Reference

### Tooling
```
python-docx      create/edit .docx programmatically
pandoc           markdown -> docx with a reference style sheet
docxtpl          template + {{placeholders}} for repeatable docs
```

### Document skeleton
```
Title (Heading 0)        one per document
H1                       chapters / major sections
H2                       subsections
Body                     paragraphs, lists
Tables                   with header row styling
```

## The rules

1. **Structure before style.** Use real heading styles, never
   bold-text-as-heading — styles drive TOC, navigation, and numbering.
2. **Consistent fonts.** Set them once at the style level (e.g. Heading +
   Normal), not per paragraph.
3. **Chinese documents:** body 宋体/仿宋 12pt (小四), headings bold; do not
   mix English fonts into CJK paragraphs unintentionally.
4. **Page setup first:** margins, page size (A4 default), then content.
5. **Tables need header repetition** across page breaks for long tables.

## Common recipes

### Build a document
```python
from docx import Document
from docx.shared import Pt, Cm

doc = Document()
doc.styles["Normal"].font.name = "宋体"
doc.styles["Normal"].font.size = Pt(12)
doc.add_heading("Quarterly Report", level=0)
doc.add_paragraph("Summary of the quarter…")
table = doc.add_table(rows=1, cols=3)
table.style = "Light Grid Accent 1"
doc.save("report.tmp.docx")  # atomic: rename afterwards
```

### Fill a template
```python
from docxtpl import DocxTemplate
tpl = DocxTemplate("contract_tpl.docx")
tpl.render({"client": "ACME", "amount": "¥120,000"})
tpl.save("contract.docx")
```

## Editing an existing document

- Read it first: dump paragraphs and tables to understand structure.
- Prefer replacing paragraph text over deleting/inserting paragraphs —
  deletions can orphan styles and section breaks.
- Never rewrite the whole file when only one section changed.

## Pitfalls

- `python-docx` cannot create TOC fields that auto-populate — insert the
  field and tell the user to press F9 in Word.
- Images: add with explicit width, or they render at native DPI.
- Fonts must exist on the reader's machine; embed or pick common ones.
- Track changes / comments are lost on programmatic rewrite — warn the
  user before overwriting a reviewed document.

## Delivery checklist

- [ ] Headings use real styles, hierarchy is consistent
- [ ] No placeholder text left ({{name}}, TODO, Lorem ipsum)
- [ ] Tables fit the page width; nothing clipped
- [ ] Saved via temp + rename, file opens cleanly
