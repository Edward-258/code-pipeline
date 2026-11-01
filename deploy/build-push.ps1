# Build + push loop for the engine image (no local Docker needed; build happens on a cluster node).
# Usage: powershell -ExecutionPolicy Bypass -File deploy\build-push.ps1 -Tag 0.1.1
#        optional: -Key <pem path>  -Node <worker public ip>
param(
    [Parameter(Mandatory = $true)][string]$Tag,
    [string]$Key = "",
    [string]$Node = "8.155.128.91"
)
$ErrorActionPreference = "Stop"
if (-not $Key) { $Key = (Get-ChildItem "E:\aliyun-pem\*81.pem" | Select-Object -First 1).FullName }

function Assert-LastExit([string]$Step) {
    if ($LASTEXITCODE -ne 0) { throw "FAILED at step: $Step (exit $LASTEXITCODE)" }
}

$repoRoot = Split-Path -Parent $PSScriptRoot

# 1. pack source (no .git)
$tar = Join-Path $env:TEMP "mp-src.tgz"
if (Test-Path $tar) { Remove-Item $tar }
Push-Location $repoRoot
tar -czf $tar go.mod go.sum main.go api config models repo engine docker
Pop-Location

# 2. build + push on the node (node has full docker + registry mirrors)
scp -i $Key $tar "root@${Node}:/opt/mp-src.tgz"
Assert-LastExit "scp source tarball"
ssh -i $Key "root@$Node" "rm -rf /opt/mp-build && mkdir -p /opt/mp-build && tar xzf /opt/mp-src.tgz -C /opt/mp-build && cd /opt/mp-build && docker build -f docker/Dockerfile -t 127.0.0.1:30500/mini-pipeline:$Tag . && docker push 127.0.0.1:30500/mini-pipeline:$Tag && docker rmi 127.0.0.1:30500/mini-pipeline:$Tag >/dev/null 2>&1 || true"
Assert-LastExit "docker build+push on node"
Remove-Item $tar

Write-Host ""
Write-Host "pushed: 127.0.0.1:30500/mini-pipeline:$Tag"
Write-Host "next:   helm upgrade mini-pipeline deploy/chart/mini-pipeline --kube-context k3s-heyuan -n pipeline --set image.tag=$Tag --set tags.engine=true"
