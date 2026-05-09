#!/bin/bash
# Python Runner Plugin - execute Python code
# Usage: run.sh run "<python code>"

COMMAND="$1"
shift

case "$COMMAND" in
  run)
    CODE="$*"
    TMPFILE=$(mktemp /tmp/python_XXXXXX.py)
    echo "$CODE" > "$TMPFILE"
    python3 "$TMPFILE" 2>&1
    EXIT_CODE=$?
    rm -f "$TMPFILE"
    exit $EXIT_CODE
    ;;
  *)
    echo "Unknown command: $COMMAND"
    echo "Available: run"
    exit 1
    ;;
esac
