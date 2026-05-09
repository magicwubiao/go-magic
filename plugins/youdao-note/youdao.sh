#!/bin/bash
# Youdao Note CLI wrapper for go-magic
#
# This script wraps the Youdao Note API/CLI.
# In production, replace the stub implementations with actual API calls.
#
# Prerequisites:
#   - Set YOUDAO_TOKEN environment variable
#   - Or configure in ~/.magic/plugins/youdao-note/config.json
#
# Commands:
#   list                List notebooks and recent notes
#   search <keyword>    Search notes by keyword
#   read <note_id>      Read a note
#   create <title>      Create a note (content from stdin or --content)
#   delete <note_id>    Delete a note

set -e

COMMAND="${1:-help}"
shift 2>/dev/null

TOKEN="${YOUDAO_TOKEN:-}"
NOTEBOOK="${YOUDAO_DEFAULT_NOTEBOOK:-default}"

# --- Stub implementations (replace with real API calls) ---
# In production, you would use curl to call the Youdao Open API:
#   https://open.youdao.com/api/doc

cmd_list() {
    # Stub: replace with actual API call
    # curl -s -H "Authorization: Bearer $TOKEN" "https://open.youdao.com/api/note/list"
    cat <<EOF
{
  "status": "ok",
  "notebooks": [
    {"name": "$NOTEBOOK", "count": 3},
    {"name": "工作笔记", "count": 12},
    {"name": "读书笔记", "count": 8}
  ],
  "recent_notes": [
    {"id": "n001", "title": "项目会议记录", "updated": "2025-05-09"},
    {"id": "n002", "title": "学习计划", "updated": "2025-05-08"},
    {"id": "n003", "title": "周报模板", "updated": "2025-05-07"}
  ],
  "_stub": true,
  "_message": "This is a demo response. Replace with real Youdao API calls."
}
EOF
}

cmd_search() {
    local keyword="${1:-}"
    if [ -z "$keyword" ]; then
        echo '{"status": "error", "message": "keyword argument required"}'
        return 1
    fi
    # Stub: curl -s -H "Authorization: Bearer $TOKEN" "https://open.youdao.com/api/note/search?q=$keyword"
    cat <<EOF
{
  "status": "ok",
  "keyword": "$keyword",
  "results": [
    {"id": "n001", "title": "项目会议记录", "snippet": "讨论了${keyword}相关内容..."},
    {"id": "n004", "title": "${keyword}调研报告", "snippet": "关于${keyword}的技术调研..."}
  ],
  "count": 2,
  "_stub": true
}
EOF
}

cmd_read() {
    local note_id="${1:-}"
    if [ -z "$note_id" ]; then
        echo '{"status": "error", "message": "note_id argument required"}'
        return 1
    fi
    # Stub: curl -s -H "Authorization: Bearer $TOKEN" "https://open.youdao.com/api/note/$note_id"
    cat <<EOF
{
  "status": "ok",
  "note": {
    "id": "$note_id",
    "title": "项目会议记录",
    "content": "# 项目会议记录\\n\\n## 参会人员\\n- 张三\\n- 李四\\n\\n## 议题\\n1. 进度同步\\n2. 下周计划",
    "created": "2025-05-09T10:00:00",
    "updated": "2025-05-09T14:30:00",
    "notebook": "$NOTEBOOK"
  },
  "_stub": true
}
EOF
}

cmd_create() {
    local title="${1:-}"
    local content="${2:-}"
    if [ -z "$title" ]; then
        echo '{"status": "error", "message": "title argument required"}'
        return 1
    fi
    # If no content arg, try reading from stdin
    if [ -z "$content" ]; then
        content=$(cat 2>/dev/null || echo "")
    fi
    # Stub: curl -s -X POST -H "Authorization: Bearer $TOKEN" -d "{\"title\":\"$title\",\"content\":\"$content\"}" "https://open.youdao.com/api/note/create"
    local new_id="n$(date +%s)"
    cat <<EOF
{
  "status": "ok",
  "note": {
    "id": "$new_id",
    "title": "$title",
    "content": "$content",
    "notebook": "$NOTEBOOK",
    "created": "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  },
  "message": "Note created successfully",
  "_stub": true
}
EOF
}

cmd_delete() {
    local note_id="${1:-}"
    if [ -z "$note_id" ]; then
        echo '{"status": "error", "message": "note_id argument required"}'
        return 1
    fi
    # Stub: curl -s -X DELETE -H "Authorization: Bearer $TOKEN" "https://open.youdao.com/api/note/$note_id"
    cat <<EOF
{
  "status": "ok",
  "message": "Note $note_id deleted",
  "_stub": true
}
EOF
}

cmd_on_load() {
    if [ -z "$TOKEN" ]; then
        echo '{"status": "warning", "message": "YOUDAO_TOKEN not set, some commands may fail"}'
    else
        echo '{"status": "ok", "message": "Youdao Note plugin loaded"}'
    fi
}

# --- Main dispatch ---
case "$COMMAND" in
    list)      cmd_list ;;
    search)    cmd_search "$@" ;;
    read)      cmd_read "$@" ;;
    create)    cmd_create "$@" ;;
    delete)    cmd_delete "$@" ;;
    on_load)   cmd_on_load ;;
    help|*)
        echo "Youdao Note Plugin - Commands: list, search, read, create, delete"
        ;;
esac
