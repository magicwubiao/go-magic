#!/usr/bin/env bash
# 列出 from..to 之间的提交，一行一条：<短 hash>\t<标题>
#
# 用法：bash scripts/collect-commits.sh [from] [to]
#   from 省略时取最近的 tag；仓库还没有 tag 时取第一个提交。
#   只读操作，不改动工作区。
set -euo pipefail

from="${1:-}"
to="${2:-HEAD}"

if [ -z "$from" ]; then
  from="$(git describe --tags --abbrev=0 2>/dev/null || git rev-list --max-parents=0 HEAD | head -n 1)"
fi

# --no-merges：合并提交的标题对使用者没有信息量；
# 用 %h%x09%s 保证 hash 与标题之间是真正的 TAB，方便下游 cut。
git log --no-merges --reverse --pretty=format:'%h%x09%s' "${from}..${to}"
echo
