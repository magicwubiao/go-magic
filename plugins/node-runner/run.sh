#!/bin/bash
# Node.js Runner Plugin - execute Node.js code
# Usage: run.sh run "<javascript code>"

COMMAND="$1"
shift

case "$COMMAND" in
  run)
    CODE="$*"
    TMPFILE=$(mktemp /tmp/node_XXXXXX.js)
    echo "$CODE" > "$TMPFILE"
    node "$TMPFILE" 2>&1
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
