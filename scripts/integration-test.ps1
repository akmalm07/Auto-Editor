[CmdletBinding()]
param(
    [Parameter(Mandatory)]
    [string]$Project
)

$ErrorActionPreference = 'Stop'

$projectPath = (Resolve-Path -LiteralPath $Project).Path
$repositoryPath = Split-Path -Parent $PSScriptRoot
$manifestPath = Join-Path $projectPath 'auto-editor.json'
$executablePath = Join-Path $repositoryPath 'auto-editor.exe'

if (-not (Test-Path -LiteralPath $manifestPath -PathType Leaf)) {
    throw "No auto-editor.json was found in $projectPath. Run init first or choose a project directory."
}

function Invoke-AutoEditor {
    param([string[]]$CommandArguments)

    Write-Host ("> auto-editor " + ($CommandArguments -join ' '))
    & $executablePath @CommandArguments
    if ($LASTEXITCODE -ne 0) {
        throw "auto-editor exited with code $LASTEXITCODE."
    }
}

Write-Host "> go build -o auto-editor.exe ./cmd/auto-editor"
Push-Location $repositoryPath
try {
    $env:GOCACHE = Join-Path $repositoryPath '.gocache'
    & go build -o $executablePath ./cmd/auto-editor
    if ($LASTEXITCODE -ne 0) {
        throw "go build exited with code $LASTEXITCODE."
    }
} finally {
    Pop-Location
}

Write-Host "> npm ci"
Push-Location $repositoryPath
try {
    & npm ci
    if ($LASTEXITCODE -ne 0) {
        throw "npm ci exited with code $LASTEXITCODE."
    }
} finally {
    Pop-Location
}

Invoke-AutoEditor @('--project', $projectPath, 'status')
Write-Host ("> npm run workflow -- --project " + $projectPath + " --through render --all-assets --go-command " + $executablePath)
Push-Location $repositoryPath
try {
    & npm run workflow -- --project $projectPath --through render --all-assets --go-command $executablePath
    if ($LASTEXITCODE -ne 0) {
        throw "LangGraph workflow exited with code $LASTEXITCODE."
    }
} finally {
    Pop-Location
}
Invoke-AutoEditor @('--project', $projectPath, 'status')
Write-Host "Integration test completed through the LangGraph workflow."
