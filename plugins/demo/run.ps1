# Demo Plugin for go-magic (PowerShell)
# Usage: run.ps1 <command> [args...]
#
# Commands:
#   hello [-Name NAME]     Say hello
#   time                   Show current time in various formats
#   stats                  Show system stats summary
#   echo <text>            Echo text (with config transforms)
#   on_load                Called when plugin loads
#   on_unload              Called when plugin unloads
#   on_session_start       Called when a chat session starts

$Command = if ($args.Count -gt 0) { $args[0] } else { "help" }
$Rest = if ($args.Count -gt 1) { $args[1..($args.Count-1)] } else { @() }

# --- Config defaults ---
$Greeting = if ($env:MAGIC_DEMO_GREETING) { $env:MAGIC_DEMO_GREETING } else { "Hello" }
$Uppercase = if ($env:MAGIC_DEMO_UPPERCASE -eq "true") { $true } else { $false }
$MaxLength = if ($env:MAGIC_DEMO_MAX_LENGTH) { [int]$env:MAGIC_DEMO_MAX_LENGTH } else { 200 }

# --- Helper functions ---
function Apply-Transforms {
    param([string]$Text)
    if ($Uppercase) { $Text = $Text.ToUpper() }
    if ($Text.Length -gt $MaxLength) { $Text = $Text.Substring(0, $MaxLength) + "..." }
    return $Text
}

function Json-Output {
    param([string]$Status, [string]$Message)
    @{ status = $Status; message = $Message } | ConvertTo-Json -Compress
}

# --- Command handlers ---
function Cmd-Hello {
    $Name = "World"
    $i = 0
    while ($i -lt $Rest.Count) {
        if ($Rest[$i] -eq "--name" -or $Rest[$i] -eq "-n") {
            $Name = $Rest[$i + 1]
            $i += 2
        } else { $i++ }
    }
    $Msg = "$Greeting, $Name! From demo plugin."
    $Msg = Apply-Transforms $Msg
    Json-Output "ok" $Msg
}

function Cmd-Time {
    $Now = Get-Date
    @{
        status = "ok"
        time = @{
            local    = $Now.ToString("yyyy-MM-dd HH:mm:ss zzz")
            utc      = $Now.ToUniversalTime().ToString("yyyy-MM-dd HH:mm:ss UTC")
            iso8601  = $Now.ToString("yyyy-MM-ddTHH:mm:ssZ")
            unix     = [int][double]::Parse((Get-Date -UFormat %s))
        }
    } | ConvertTo-Json -Compress
}

function Cmd-Stats {
    @{
        status  = "ok"
        system  = @{
            os       = [System.Environment]::OSVersion.ToString()
            hostname = [System.Environment]::MachineName
            uptime   = (Get-Date) - ([System.Diagnostics.Process]::GetCurrentProcess().StartTime)
        }
    } | ConvertTo-Json -Compress
}

function Cmd-Echo {
    $Text = $Rest -join " "
    if (-not $Text) {
        Json-Output "error" "No text provided"
        return
    }
    $Text = Apply-Transforms $Text
    Json-Output "ok" $Text
}

function Cmd-OnLoad {
    Json-Output "ok" "Demo plugin loaded"
}

function Cmd-OnUnload {
    Json-Output "ok" "Demo plugin unloaded"
}

function Cmd-OnSessionStart {
    $SessionId = if ($Rest.Count -gt 0) { $Rest[0] } else { "cli" }
    Json-Output "ok" "Session $SessionId started"
}

# --- Main dispatch ---
switch ($Command) {
    "hello"            { Cmd-Hello }
    "time"             { Cmd-Time }
    "stats"            { Cmd-Stats }
    "echo"             { Cmd-Echo }
    "on_load"          { Cmd-OnLoad }
    "on_unload"        { Cmd-OnUnload }
    "on_session_start" { Cmd-OnSessionStart }
    "help"             { Write-Host "Demo Plugin - Commands: hello, time, stats, echo" }
    default            { Json-Output "error" "Unknown command: $Command"; exit 1 }
}
