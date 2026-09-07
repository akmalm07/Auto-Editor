[CmdletBinding()]
param(
    [Parameter(Mandatory)]
    [string]$Project,

    [string]$Repository = (Split-Path -Parent $PSScriptRoot),

    [ValidateSet('ingest', 'align', 'analyze-assets', 'analyze-style', 'plan')]
    [string]$Through = 'plan',

    [switch]$NoSemantic,

    [switch]$SkipBuild
)

$ErrorActionPreference = 'Stop'

$projectPath = (Resolve-Path -LiteralPath $Project).Path
$repositoryPath = (Resolve-Path -LiteralPath $Repository).Path
$manifestPath = Join-Path $projectPath 'auto-editor.json'
$executablePath = Join-Path $repositoryPath 'auto-editor.exe'

if (-not (Test-Path -LiteralPath $manifestPath -PathType Leaf)) {
    throw "No auto-editor.json was found in $projectPath. Run init first or choose a project directory."
}

function Invoke-AutoEditor {
    param([string[]]$CommandArguments)

    Write-Host ("> auto-editor " + ($CommandArguments -join ' '))
    try {
        & $executablePath @CommandArguments
    } catch {
        throw "Could not start $executablePath. Windows Application Control may have blocked the newly built executable. $_"
    }
    if ($LASTEXITCODE -ne 0) {
        throw "auto-editor exited with code $LASTEXITCODE."
    }
}

if (-not $SkipBuild) {
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
}

if (-not (Test-Path -LiteralPath $executablePath -PathType Leaf)) {
    throw "auto-editor executable was not found at $executablePath. Remove -SkipBuild or provide a valid -Repository path."
}

Invoke-AutoEditor @('--project', $projectPath, 'status')

$pipelineArguments = @('--project', $projectPath, 'run', '--through', $Through)
if ($NoSemantic) {
    $pipelineArguments += '--no-semantic'
}
Invoke-AutoEditor $pipelineArguments

Invoke-AutoEditor @('--project', $projectPath, 'status')
Write-Host "Integration test completed through $Through."
