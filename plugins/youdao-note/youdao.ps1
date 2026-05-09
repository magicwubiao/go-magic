# Youdao Note CLI wrapper for go-magic (PowerShell)
#
# Commands:
#   list                List notebooks and recent notes
#   search <keyword>    Search notes by keyword
#   read <note_id>      Read a note
#   create <title> <content> Create a note
#   delete <note_id>    Delete a note

$Command = if ($args.Count -gt 0) { $args[0] } else { "help" }
$Rest = if ($args.Count -gt 1) { $args[1..($args.Count-1)] } else { @() }

$Token = $env:YOUDAO_TOKEN
$Notebook = if ($env:YOUDAO_DEFAULT_NOTEBOOK) { $env:YOUDAO_DEFAULT_NOTEBOOK } else { "default" }

function Cmd-List {
    # Stub: replace with actual API call
    # Invoke-RestMethod -Headers @{Authorization="Bearer $Token"} -Uri "https://open.youdao.com/api/note/list"
    @{
        status = "ok"
        notebooks = @(
            @{ name = $Notebook; count = 3 }
            @{ name = "工作笔记"; count = 12 }
            @{ name = "读书笔记"; count = 8 }
        )
        recent_notes = @(
            @{ id = "n001"; title = "项目会议记录"; updated = "2025-05-09" }
            @{ id = "n002"; title = "学习计划"; updated = "2025-05-08" }
            @{ id = "n003"; title = "周报模板"; updated = "2025-05-07" }
        )
        _stub = $true
        _message = "This is a demo response. Replace with real Youdao API calls."
    } | ConvertTo-Json -Compress
}

function Cmd-Search {
    $Keyword = if ($Rest.Count -gt 0) { $Rest[0] } else { "" }
    if (-not $Keyword) {
        @{ status = "error"; message = "keyword argument required" } | ConvertTo-Json -Compress
        return
    }
    @{
        status = "ok"
        keyword = $Keyword
        results = @(
            @{ id = "n001"; title = "项目会议记录"; snippet = "讨论了${Keyword}相关内容..." }
            @{ id = "n004"; title = "${Keyword}调研报告"; snippet = "关于${Keyword}的技术调研..." }
        )
        count = 2
        _stub = $true
    } | ConvertTo-Json -Compress
}

function Cmd-Read {
    $NoteId = if ($Rest.Count -gt 0) { $Rest[0] } else { "" }
    if (-not $NoteId) {
        @{ status = "error"; message = "note_id argument required" } | ConvertTo-Json -Compress
        return
    }
    @{
        status = "ok"
        note = @{
            id = $NoteId
            title = "项目会议记录"
            content = "# 项目会议记录`n`n## 参会人员`n- 张三`n- 李四`n`n## 议题`n1. 进度同步`n2. 下周计划"
            created = "2025-05-09T10:00:00"
            updated = "2025-05-09T14:30:00"
            notebook = $Notebook
        }
        _stub = $true
    } | ConvertTo-Json -Compress
}

function Cmd-Create {
    $Title = if ($Rest.Count -gt 0) { $Rest[0] } else { "" }
    $Content = if ($Rest.Count -gt 1) { $Rest[1] } else { "" }
    if (-not $Title) {
        @{ status = "error"; message = "title argument required" } | ConvertTo-Json -Compress
        return
    }
    $NewId = "n" + [int][double]::Parse((Get-Date -UFormat %s))
    @{
        status = "ok"
        note = @{
            id = $NewId
            title = $Title
            content = $Content
            notebook = $Notebook
            created = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")
        }
        message = "Note created successfully"
        _stub = $true
    } | ConvertTo-Json -Compress
}

function Cmd-Delete {
    $NoteId = if ($Rest.Count -gt 0) { $Rest[0] } else { "" }
    if (-not $NoteId) {
        @{ status = "error"; message = "note_id argument required" } | ConvertTo-Json -Compress
        return
    }
    @{
        status = "ok"
        message = "Note $NoteId deleted"
        _stub = $true
    } | ConvertTo-Json -Compress
}

function Cmd-OnLoad {
    if (-not $Token) {
        @{ status = "warning"; message = "YOUDAO_TOKEN not set, some commands may fail" } | ConvertTo-Json -Compress
    } else {
        @{ status = "ok"; message = "Youdao Note plugin loaded" } | ConvertTo-Json -Compress
    }
}

switch ($Command) {
    "list"    { Cmd-List }
    "search"  { Cmd-Search }
    "read"    { Cmd-Read }
    "create"  { Cmd-Create }
    "delete"  { Cmd-Delete }
    "on_load" { Cmd-OnLoad }
    default   { Write-Host "Youdao Note Plugin - Commands: list, search, read, create, delete" }
}
