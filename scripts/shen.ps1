<#
.SYNOPSIS
  蜃楼 Shen 的 Windows 入口（PowerShell 5.1+ / PowerShell 7；也可在 Linux / macOS 的 pwsh 下使用）。
  与 scripts/shen.sh 的常用子命令一一对应，只依赖 Docker Desktop（不需要 make / bash / Go / Node）。

.EXAMPLE
  ./scripts/shen.ps1 up                       # 起全部模块
  ./scripts/shen.ps1 up -Profile honeypot     # 只起蜜罐（core 永远随行）
  ./scripts/shen.ps1 up -Profile frontend,backend
  ./scripts/shen.ps1 up -Env dev              # 开发形态：Vite 热更新 + 调试端口
  ./scripts/shen.ps1 status | logs console-api | restart | down
  ./scripts/shen.ps1 smoke | traffic | doctor | verify   # 验证链路 / 完整流量 / 接入自检 / 端到端

.NOTES
  若提示「无法加载脚本，因为在此系统上禁止运行脚本」，执行一次：
    Set-ExecutionPolicy -Scope CurrentUser RemoteSigned
  建议把仓库放在 WSL2 文件系统（\\wsl$\...）或本地 NTFS 磁盘；网络盘上的绑定挂载非常慢。
#>
[CmdletBinding()]
param(
  [Parameter(Position = 0)]
  [ValidateSet('up', 'down', 'ps', 'status', 'logs', 'restart', 'smoke', 'traffic', 'doctor', 'verify', 'help')]
  [string]$Command = 'help',

  # 形态：dev / prod / verify（参数名 -Env；内部用 $Mode，避免与 $env: 驱动器混淆）
  [Alias('Env')]
  [ValidateSet('', 'dev', 'prod', 'verify')]
  [string]$Mode = '',

  # 模块：all / frontend / backend / honeypot / analysis / deception / console / demo（参数名 -Profile；
  # 内部用 $Modules，避免遮蔽 PowerShell 自动变量 $PROFILE）
  [Alias('Profile')]
  [string[]]$Modules = @(),

  [Parameter(ValueFromRemainingArguments = $true)]
  [string[]]$Rest = @()
)

$ErrorActionPreference = 'Stop'
$Root = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$EnvFile = Join-Path $Root '.env'
$RunDir = Join-Path ([System.IO.Path]::GetTempPath()) 'shen'
New-Item -ItemType Directory -Force -Path $RunDir | Out-Null

function Fail([string]$msg) { Write-Host "shen: $msg" -ForegroundColor Red; exit 1 }

function Assert-Docker {
  if (-not (Get-Command docker -ErrorAction SilentlyContinue)) { Fail '找不到 docker —— 请先安装并启动 Docker Desktop' }
  docker info *> $null
  if ($LASTEXITCODE -ne 0) { Fail 'Docker 守护进程没跑 —— 先启动 Docker Desktop' }
  docker compose version *> $null
  if ($LASTEXITCODE -ne 0) { Fail '需要 Docker Compose v2（docker compose）' }
}

function New-Secret {
  $bytes = New-Object byte[] 24
  [System.Security.Cryptography.RandomNumberGenerator]::Create().GetBytes($bytes)
  ([Convert]::ToBase64String($bytes) -replace '[/+=]', '').Substring(0, 30)
}

# 首次运行从模板生成 .env（随机初始口令与只读令牌）；已有 .env 时绝不覆盖。写 UTF-8 无 BOM + LF。
function Initialize-EnvFile {
  if (Test-Path $EnvFile) { return }
  $template = Join-Path $Root '.env.example'
  if (-not (Test-Path $template)) { Fail '缺少 .env.example' }
  $content = (Get-Content -Raw -Encoding UTF8 $template) `
    -replace '(?m)^SHEN_CONSOLE_API_TOKEN=.*$', "SHEN_CONSOLE_API_TOKEN=$(New-Secret)"
  $content = $content -replace "`r`n", "`n"
  [System.IO.File]::WriteAllText($EnvFile, $content, (New-Object System.Text.UTF8Encoding($false)))
  Write-Host "已生成 $EnvFile（管理员默认 admin/admin 便于验证，首登强制改密；只读令牌为随机值）" -ForegroundColor Cyan
}

function Get-EnvValue([string]$key, [string]$default = '') {
  $fromShell = [Environment]::GetEnvironmentVariable($key)
  if ($fromShell) { return $fromShell }
  if (Test-Path $EnvFile) {
    $line = Get-Content -Encoding UTF8 $EnvFile | Where-Object { $_ -match "^$key=" } | Select-Object -Last 1
    if ($line) { $v = ($line -split '=', 2)[1].Trim(); if ($v) { return $v } }
  }
  return $default
}

function Invoke-Compose([string[]]$Arguments) {
  Initialize-EnvFile
  $argv = @('compose', '--project-directory', $Root)
  $envArg = $EnvFile
  if ($Modules.Count -gt 0) {
    # 显式指定模块时去掉 .env 里的 COMPOSE_PROFILES（否则两者叠加，「只起蜜罐」会变成「全部」）。
    $envArg = Join-Path $RunDir 'compose.env'
    Get-Content -Encoding UTF8 $EnvFile | Where-Object { $_ -notmatch '^COMPOSE_PROFILES=' } | Set-Content -Encoding UTF8 $envArg
  }
  $argv += @('--env-file', $envArg, '-f', (Join-Path $Root 'compose.yaml'))
  switch ($Mode) {
    'dev' { $argv += @('-f', (Join-Path $Root 'deploy/docker/compose.dev.yaml')) }
    'prod' { $argv += @('-f', (Join-Path $Root 'deploy/docker/compose.prod.yaml')) }
    'verify' { $argv += @('-f', (Join-Path $Root 'deploy/docker/compose.verify-mirage.yaml')) }
  }
  foreach ($p in ($Modules -join ',').Split(',', [StringSplitOptions]::RemoveEmptyEntries)) { $argv += @('--profile', $p.Trim()) }
  & docker @argv @Arguments
  if ($LASTEXITCODE -ne 0) { Fail "docker compose $($Arguments -join ' ') 失败（退出码 $LASTEXITCODE）" }
}

function Get-ConsoleUrl { "http://127.0.0.1:$(Get-EnvValue 'SHEN_CONSOLE_PORT' '19444')" }
function Get-EntryUrl { "http://127.0.0.1:$(Get-EnvValue 'SHEN_HTTP_PORT' '18080')" }

function Test-WantsConsole {
  if ($Modules.Count -eq 0) { return $true }
  $all = ($Modules -join ',').Split(',') | ForEach-Object { $_.Trim() }
  return [bool]($all | Where-Object { $_ -in @('all', 'frontend', 'console') })
}

function Wait-Ready {
  if (-not (Test-WantsConsole)) { return }
  $url = "$(Get-ConsoleUrl)/healthz"
  for ($i = 0; $i -lt 90; $i++) {
    try { Invoke-WebRequest -UseBasicParsing -TimeoutSec 2 -Uri $url | Out-Null; return } catch { Start-Sleep -Seconds 1 }
  }
  Invoke-Compose @('ps')
  Fail '管控台 90 秒内未就绪（scripts/shen.ps1 logs console-api console-ui 查看原因）'
}

function Invoke-Api([string]$path) {
  $token = Get-EnvValue 'SHEN_CONSOLE_API_TOKEN'
  if (-not $token) { Fail '未设置 SHEN_CONSOLE_API_TOKEN，无法读取管控台接口' }
  Invoke-RestMethod -TimeoutSec 5 -Headers @{ Authorization = "Bearer $token" } -Uri "$(Get-ConsoleUrl)$path"
}

function Show-Urls {
  $user = Get-EnvValue 'SHEN_CONSOLE_BOOTSTRAP_USER' 'admin'
  Write-Host ''
  Write-Host '  ✅ 已就绪' -ForegroundColor Green
  Write-Host "     管控台     $(Get-ConsoleUrl)/    账号 $user / 默认口令 admin（首登强制改密）"
  Write-Host "     业务入口   $(Get-EntryUrl)/      （经引擎；影子模式：只观测、不处置）"
  Write-Host ''
  Write-Host "  造点流量：Invoke-WebRequest -UseBasicParsing -UserAgent 'HeadlessChrome/120' $(Get-EntryUrl)/.git/config"
}

switch ($Command) {
  'up' {
    Assert-Docker
    Invoke-Compose (@('up', '-d', '--build') + $Rest)
    Wait-Ready
    Show-Urls
  }
  'restart' {
    # 整栈重建：core 是网络命名空间的持有者，单独重启它会让兄弟容器留在旧命名空间。
    Assert-Docker
    Invoke-Compose (@('up', '-d', '--build', '--force-recreate') + $Rest)
    Wait-Ready
    Write-Host '✅ 已整栈重建' -ForegroundColor Green
  }
  'down' { Assert-Docker; Invoke-Compose (@('down') + $Rest) }
  'ps' { Assert-Docker; Invoke-Compose @('ps') }
  'logs' { Assert-Docker; Invoke-Compose (@('logs', '--tail=80') + $Rest) }
  'status' {
    Assert-Docker
    Invoke-Compose @('ps')
    Invoke-Api '/api/v1/system/status' | ConvertTo-Json -Depth 5
  }
  'smoke' {
    foreach ($spec in @(@('HeadlessChrome/120', '/.git/config'), @('sqlmap/1.7', '/etc/passwd'), @('Mozilla/5.0', '/'))) {
      try {
        $r = Invoke-WebRequest -UseBasicParsing -TimeoutSec 10 -UserAgent $spec[0] -Uri "$(Get-EntryUrl)$($spec[1])"
        $code = $r.StatusCode
      } catch { $code = $_.Exception.Response.StatusCode.value__ }
      Write-Host ("  HTTP {0}  {1}  {2}" -f $code, $spec[0], $spec[1])
    }
    Start-Sleep -Seconds 1
    Invoke-Api '/api/v1/deception/flow?limit=5' | ForEach-Object {
      Write-Host ("    {0,-12} {1,-4} {2,-16} score={3} 归属地={4}" -f $_.action, $_.method, $_.path, $_.score, $_.geo.label)
    }
  }
  'traffic' {
    # 伪造流量 + 从观测面核对判定（完整验证；参数原样传给 send.py，如 --check-l4 --explain）
    # 赋值加括号：值是函数引用（环境变量透传），不是实值 —— 也不该长成密钥赋值的形态（secrets-check 规则②）。
    $env:SHEN_CONSOLE_API_TOKEN = (Get-EnvValue 'SHEN_CONSOLE_API_TOKEN')
    python (Join-Path $Root 'scripts/traffic/send.py') @('--entry', (Get-EntryUrl), '--console', (Get-ConsoleUrl)) + $Rest
    if ($LASTEXITCODE -ne 0) { Fail 'traffic 验证未通过（详见上方输出）' }
  }
  'doctor' {
    # 接入自检（INT-17 五项：body 可读性 / TLS 终结 / 会话粘性 / 后端可区分 / 引擎在路径上）
    $env:SHEN_CONSOLE_API_TOKEN = (Get-EnvValue 'SHEN_CONSOLE_API_TOKEN')
    python (Join-Path $Root 'scripts/doctor/doctor.py') @('--entry', (Get-EntryUrl), '--console', (Get-ConsoleUrl)) + $Rest
    if ($LASTEXITCODE -ne 0) { Fail 'doctor 自检未通过（详见上方输出）' }
  }
  'verify' {
    # 一键端到端：容器与观测面 → 伪造流量 + L4 核对 → 报告
    Assert-Docker
    $env:SHEN_CONSOLE_API_TOKEN = (Get-EnvValue 'SHEN_CONSOLE_API_TOKEN')
    Invoke-Compose @('ps')
    $report = Join-Path $RunDir ("verify-{0}.json" -f (Get-Date -Format 'yyyyMMdd-HHmmss'))
    python (Join-Path $Root 'scripts/traffic/send.py') @('--entry', (Get-EntryUrl), '--console', (Get-ConsoleUrl), '--check-l4', '--check-graph', '--explain', '--report', $report) + $Rest
    if ($LASTEXITCODE -ne 0) { Fail "verify 未通过；报告：$report" }
    Write-Host "报告：$report"
  }
  default { Get-Help $PSCommandPath -Detailed }
}
