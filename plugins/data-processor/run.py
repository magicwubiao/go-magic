#!/usr/bin/env python3
"""
Data Processor Plugin for go-magic
Supports CSV and JSON data processing with statistics
"""

import sys
import json
import csv
from io import StringIO
from typing import Dict, List, Any
from collections import Counter
import statistics


def process_csv(data: str) -> Dict[str, Any]:
    """Process CSV data and return statistics."""
    reader = csv.DictReader(StringIO(data))
    rows = list(reader)
    
    if not rows:
        return {"error": "No data in CSV"}
    
    # Get numeric columns
    numeric_cols = []
    text_cols = []
    
    for col in rows[0].keys():
        values = [float(row[col]) for row in rows if row[col].replace('.', '').replace('-', '').isdigit()]
        if len(values) == len(rows):
            numeric_cols.append((col, values))
        else:
            text_cols.append(col)
    
    result = {
        "row_count": len(rows),
        "column_count": len(rows[0]),
        "columns": list(rows[0].keys()),
    }
    
    # Statistics for numeric columns
    if numeric_cols:
        result["numeric_columns"] = {}
        for col, values in numeric_cols:
            result["numeric_columns"][col] = {
                "sum": sum(values),
                "mean": statistics.mean(values),
                "median": statistics.median(values),
                "min": min(values),
                "max": max(values),
                "stdev": statistics.stdev(values) if len(values) > 1 else 0,
            }
    
    # Top values for text columns
    if text_cols:
        result["text_columns"] = {}
        for col in text_cols:
            values = [row[col] for row in rows if row[col]]
            counter = Counter(values)
            result["text_columns"][col] = {
                "unique_count": len(counter),
                "top_values": dict(counter.most_common(5)),
            }
    
    return result


def process_json(data: str) -> Dict[str, Any]:
    """Process JSON data and return statistics."""
    try:
        parsed = json.loads(data)
    except json.JSONDecodeError as e:
        return {"error": f"Invalid JSON: {e}"}
    
    if isinstance(parsed, list):
        return {
            "type": "array",
            "length": len(parsed),
            "sample": parsed[0] if parsed else None,
        }
    elif isinstance(parsed, dict):
        return {
            "type": "object",
            "keys": list(parsed.keys()),
            "value_types": {k: type(v).__name__ for k, v in parsed.items()},
        }
    else:
        return {"type": type(parsed).__name__, "value": parsed}


def main():
    if len(sys.argv) < 3:
        print("Usage: run.py <format:csv|json> <data>")
        sys.exit(1)
    
    format_type = sys.argv[1].lower()
    data = sys.argv[2]
    
    if format_type == "csv":
        result = process_csv(data)
    elif format_type == "json":
        result = process_json(data)
    else:
        print(f"Unsupported format: {format_type}")
        sys.exit(1)
    
    print(json.dumps(result, indent=2))


if __name__ == "__main__":
    main()
