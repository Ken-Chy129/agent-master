# agent-master installer for Windows — downloads the release binary.
#
#   irm https://raw.githubusercontent.com/Ken-Chy129/agent-master/main/install.ps1 | iex
#
# Env:
#   AGENT_MASTER_VERSION  specific version (default: latest release)
#   INSTALL_DIR           install dir (default: %USERPROFILE%\.local\bin)
#
# Requires Windows 10 1903+ (for the windowless background start).

$ErrorActionPreference = 'Stop'
# Windows PowerShell 5.1 defaults to TLS 1.0, which GitHub rejects.
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12

$repo = 'Ken-Chy129/agent-master'
$bin = 'agent-master'
$installDir = if ($env:INSTALL_DIR) { $env:INSTALL_DIR } else { Join-Path $HOME '.local\bin' }

$arch = switch ($env:PROCESSOR_ARCHITECTURE) {
    'AMD64' { 'amd64' }
    'ARM64' { 'arm64' }
    default { throw "unsupported architecture: $env:PROCESSOR_ARCHITECTURE" }
}

$ver = $env:AGENT_MASTER_VERSION
if (-not $ver) {
    # Prefer the github.com redirect: /releases/latest -> /releases/tag/vX.Y.Z.
    # Not subject to the unauthenticated api.github.com rate limit.
    try {
        $req = [System.Net.WebRequest]::Create("https://github.com/$repo/releases/latest")
        $req.AllowAutoRedirect = $false
        $resp = $req.GetResponse()
        $loc = $resp.Headers['Location']
        $resp.Close()
        if ($loc -match '/releases/tag/v?([^/]+)$') { $ver = $Matches[1] }
    } catch {}
}
if (-not $ver) {
    # Fallback: the API (may be rate-limited when unauthenticated).
    try {
        $rel = Invoke-RestMethod "https://api.github.com/repos/$repo/releases/latest"
        $ver = $rel.tag_name -replace '^v', ''
    } catch {}
}
if (-not $ver) {
    throw 'cannot determine latest version (GitHub may be rate-limiting). Retry, or pin it: $env:AGENT_MASTER_VERSION = "0.1.0"'
}

$asset = "$bin-windows-$arch.exe"
$base = "https://github.com/$repo/releases/download/v$ver"
$tmp = Join-Path ([IO.Path]::GetTempPath()) "$asset.$PID"

Write-Host "Downloading $bin v$ver ($asset)..."
Invoke-WebRequest -Uri "$base/$asset" -OutFile $tmp -UseBasicParsing

# Verify the consolidated checksum manifest. Fall back to the legacy per-asset
# checksum used by older releases.
$expected = $null
try {
    $sumText = (Invoke-WebRequest -Uri "$base/SHA256SUMS" -UseBasicParsing).Content
} catch {
    $sumText = $null
}
if ($sumText) {
    foreach ($line in ($sumText -split '\r?\n')) {
        if ($line -match '^([a-fA-F0-9]{64})\s+\*?(.+)$' -and $Matches[2].Trim() -eq $asset) {
            $expected = $Matches[1]
            break
        }
    }
    if (-not $expected) {
        Remove-Item -Force $tmp
        throw "SHA256SUMS has no entry for $asset"
    }
} else {
    $legacyText = $null
    try {
        $legacyText = (Invoke-WebRequest -Uri "$base/$asset.sha256" -UseBasicParsing).Content
    } catch {}
    if ($legacyText) {
        foreach ($line in ($legacyText -split '\r?\n')) {
            if ($line -match '^([a-fA-F0-9]{64})\s+\*?(.+)$' -and $Matches[2].Trim() -eq $asset) {
                $expected = $Matches[1]
                break
            }
        }
        if (-not $expected) {
            Remove-Item -Force $tmp
            throw "invalid checksum file for $asset"
        }
    } else {
        Write-Host 'Warning: no checksum published for this asset; skipping verification.'
    }
}
if ($expected) {
    $actual = (Get-FileHash -Algorithm SHA256 -Path $tmp).Hash.ToLower()
    if ($actual -ne $expected.ToLower()) {
        Remove-Item -Force $tmp
        throw "checksum mismatch: expected $expected, got $actual"
    }
}

New-Item -ItemType Directory -Force -Path $installDir | Out-Null
$dest = Join-Path $installDir "$bin.exe"
Move-Item -Force $tmp $dest

# Make sure the install dir is on the user PATH (takes effect in new shells).
$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
if (($userPath -split ';') -notcontains $installDir) {
    [Environment]::SetEnvironmentVariable('Path', "$userPath;$installDir", 'User')
    $env:Path = "$env:Path;$installDir"
    Write-Host "Added $installDir to your user PATH (restart other shells to pick it up)."
}

Write-Host ''
Write-Host "✓ Installed $bin v$ver to $dest"
Write-Host ''
Write-Host 'Get started:'
Write-Host "  $bin start"
