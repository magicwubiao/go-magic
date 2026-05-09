#!/bin/bash
# Demo Plugin for go-magic
# Usage: run.sh <command> [args...]
#
# Commands:
#   hello [--name NAME]   Say hello
#   time                  Show current time in various formats
#   stats                 Show system stats summary
#   echo <text>           Echo text (with config transforms)
#   on_load               Called when plugin loads
#   on_unload             Called when plugin unloads
#   on_session_start      Called when a chat session starts

COMMAND="${1:-help}"
shift 2>/dev/null

# --- Config defaults (can be overridden by plugin config) ---
GREETING="${MAGIC_DEMO_GREETING:-Hello}"
UPPERCASE="${MAGIC_DEMO_UPPERCASE:-false}"
MAX_LENGTH="${MAGIC_DEMO_MAX_LENGTH:-200}"

# --- Helper functions ---
apply_transforms() {
    local text="$1"
    if [ "$UPPERCASE" = "true" ]; then
        text=$(echo "$text" | tr '[:lower:]' '[:upper:]')
    fi
    if [ ${#text} -gt "$MAX_LENGTH" ]; then
        text="${text:0:$MAX_LENGTH}..."
    fi
    echo "$text"
}

json_output() {
    local status="$1"
    local message="$2"
    echo "{\"status\": \"$status\", \"message\": \"$message\"}"
}

# --- Command handlers ---
cmd_hello() {
    local name="World"
    while [ $# -gt 0 ]; do
        case "$1" in
            --name|-n) name="$2"; shift 2 ;;
            *) shift ;;
        esac
    done
    local msg="$GREETING, $name! 👋 From demo plugin."
    msg=$(apply_transforms "$msg")
    json_output "ok" "$msg"
}

cmd_time() {
    local iso8601 utc timestamp
    iso8601=$(date -u +"%Y-%m-%dT%H:%M:%SZ" 2>/dev/null || date +"%Y-%m-%dT%H:%M:%S")
    utc=$(date -u +"%Y-%m-%d %H:%M:%S UTC" 2>/dev/null || date)
    timestamp=$(date +%s 2>/dev/null || echo "0")
    local local_tz
    local_tz=$(date +"%Y-%m-%d %H:%M:%S %Z" 2>/dev/null || date)
    cat <<EOF
{
  "status": "ok",
  "time": {
    "local": "$local_tz",
    "utc": "$utc",
    "iso8601": "$iso8601",
    "unix": $timestamp
  }
}
EOF
}

cmd_stats() {
    local os_name hostname uptime_str
    os_name=$(uname -s 2>/dev/null || echo "unknown")
    hostname=$(hostname 2>/dev/null || echo "unknown")
    uptime_str=$(uptime 2>/dev/null || echo "unknown")
    cat <<EOF
{
  "status": "ok",
  "system": {
    "os": "$os_name",
    "hostname": "$hostname",
    "uptime": $(echo "$uptime_str" | sed 's/"/\\"/g' | awk '{printf "\"%s\"", $0}')
  }
}
EOF
}

cmd_echo() {
    local text="${*:-}"
    if [ -z "$text" ]; then
        json_output "error" "No text provided"
        return 1
    fi
    text=$(apply_transforms "$text")
    json_output "ok" "$text"
}

cmd_on_load() {
    echo "{\"status\": \"ok\", \"event\": \"on_load\", \"message\": \"Demo plugin loaded\"}"
}

cmd_on_unload() {
    echo "{\"status\": \"ok\", \"event\": \"on_unload\", \"message\": \"Demo plugin unloaded\"}"
}

cmd_on_session_start() {
    local session_id="${1:-cli}"
    echo "{\"status\": \"ok\", \"event\": \"on_session_start\", \"session_id\": \"$session_id\"}"
}

# --- Main dispatch ---
case "$COMMAND" in
    hello)           cmd_hello "$@" ;;
    time)            cmd_time ;;
    stats)           cmd_stats ;;
    echo)            cmd_echo "$@" ;;
    on_load)         cmd_on_load ;;
    on_unload)       cmd_on_unload ;;
    on_session_start) cmd_on_session_start "$@" ;;
    help|--help|-h)
        echo "Demo Plugin - Available commands: hello, time, stats, echo"
        echo "Lifecycle hooks: on_load, on_unload, on_session_start"
        ;;
    *)
        json_output "error" "Unknown command: $COMMAND"
        exit 1
        ;;
esac
