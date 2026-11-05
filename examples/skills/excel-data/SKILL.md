---
name: excel-data
description: "Use when creating, reading, cleaning, or analyzing spreadsheets — xlsx/csv data entry, formulas, pivot tables, charts, deduplication, or formatting a report table for delivery."
version: 1.1.0
author: go-magic
license: MIT
tags: [excel, spreadsheet, data, csv, office]
tools: [execute_command, read_file, write_file, file_edit, search_in_files]
---

# Excel / Spreadsheet Guide

How to work with spreadsheet files reliably — reading, cleaning, computing, and delivering.

## When to Use

Load this skill when:
- Reading or extracting data from .xlsx / .xls / .csv files
- Creating a new workbook or adding sheets, columns, or charts
- Cleaning data: dedupe, trim whitespace, fix date/number formats
- Building formulas, pivot-table-style summaries, or charts
- Preparing a report table for delivery

## Quick Reference

### File formats
```
.xlsx   Excel workbook (use a real xlsx library, not plain text)
.csv    plain text, check encoding (UTF-8 vs GBK for Chinese data)
.xls    legacy format — convert to .xlsx when possible
```

### Tooling choice
```
Python + openpyxl    read/write .xlsx, keep formatting, charts
Python + pandas      bulk cleaning, joins, groupby summaries
csv module           lightweight CSV without dependencies
```

## The rules

1. **Inspect before writing.** Read header row, sheet names, and a data
   sample first. Never assume column meanings from names alone.
2. **Never edit the user's only copy in place.** Work on a copy, or write
   to a new file, then let the user replace the original.
3. **Atomic writes.** Write to a temp file and rename — a half-written
   workbook is unreadable by Excel.
4. **Preserve what you did not touch.** Column widths, number formats,
   and other sheets must survive the edit.
5. **CSV encoding.** Detect encoding before parsing; when writing for
   Excel-on-Windows users, prefer UTF-8 with BOM.

## Common recipes

### Read a summary
```python
import pandas as pd
df = pd.read_excel("input.xlsx", sheet_name="Sales")
print(df.shape, list(df.columns))
print(df.describe(include="all"))
```

### Clean duplicates
```python
df = df.drop_duplicates(subset=["order_id"], keep="last")
df["amount"] = pd.to_numeric(df["amount"], errors="coerce")
```

### Write a styled report
```python
from openpyxl import Workbook
from openpyxl.styles import Font, PatternFill

wb = Workbook()
ws = wb.active
ws.append(["Region", "Sales"])
for c in ws[1]:
    c.font = Font(bold=True)
    c.fill = PatternFill("solid", fgColor="DDDDDD")
wb.save("report.tmp.xlsx")  # then rename to report.xlsx
```

## Pivot-style summary without pivot tables

```python
pivot = df.pivot_table(index="Region", columns="Quarter",
                       values="Sales", aggfunc="sum", fill_value=0)
pivot.to_excel("summary.xlsx")
```

## Pitfalls

- Dates: Excel stores dates as serial numbers. Check the number format
  before treating a column as text.
- Merged cells break naive readers — unmerge or fill values first.
- Formulas vs values: `openpyxl` reads formulas (`data_only=False`) or
  cached values (`data_only=True`) — pick deliberately.
- Large files: load with `read_only=True` mode or stream CSV in chunks.

## Delivery checklist

- [ ] Opened the output once to verify it is not corrupt
- [ ] Header row formatted, columns wide enough to read
- [ ] Numbers have consistent decimal places; dates one format
- [ ] Told the user what was changed and what was left alone
