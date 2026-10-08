# ローカル開発環境を1コマンドで起動する。
# db(docker) → DBの移行 → Go API → frontend(vite) の順に立ち上げ、
# Go APIとfrontendはそれぞれ別ウィンドウで起動してログを分離する。
#
# 事前に必要なもの: Docker Desktop、Go、Node.js（npm）
# 使い方: .\dev.ps1

$ErrorActionPreference = "Stop"
$root = $PSScriptRoot
$serverEnv = "$root\server\.env"

if (-not (Test-Path $serverEnv)) {
    Write-Host "server\.env が見つかりません。server\.env.example をコピーして作成してください。" -ForegroundColor Red
    exit 1
}

# 画面側の既定は、リポジトリの frontend\.env.development にある（frontend\.env は不要）。
# それより優先される設定が残っていると、画面が古い接続先（例: :8000）へ行き、APIに接続できなくなる。
# 存在だけを調べ、中身は読まない
if (Test-Path "$root\frontend\.env.development.local") {
    Write-Host "警告: frontend\.env.development.local があります。接続先を上書きしていないか確認してください。" -ForegroundColor Yellow
}
if ($env:VITE_API_BASE_URL) {
    Write-Host "警告: 環境変数 VITE_API_BASE_URL が設定されており、画面の接続先を上書きします。" -ForegroundColor Yellow
}

# Go版は環境変数だけを読む。server\.env を、このウィンドウの環境変数へ読み込む。
# 後で起動する別ウィンドウ（Go API）も、この環境変数を引き継ぐ
Get-Content $serverEnv | ForEach-Object {
    if ($_ -match '^\s*([A-Za-z_][A-Za-z0-9_]*)=(.*)$') {
        Set-Item -Path "Env:$($Matches[1])" -Value $Matches[2]
    }
}

Write-Host "[1/4] DBコンテナを起動中..." -ForegroundColor Cyan
docker compose -f "$root\docker-compose.yml" up -d db
if ($LASTEXITCODE -ne 0) {
    Write-Host "docker compose up に失敗しました。Docker Desktopが起動しているか確認してください。" -ForegroundColor Red
    exit 1
}

Write-Host "[1/4] DBのヘルスチェック待ち..." -ForegroundColor Cyan
$dbContainer = docker compose -f "$root\docker-compose.yml" ps -q db
if (-not $dbContainer) {
    Write-Host "DBコンテナIDを取得できませんでした。docker compose ps db で状態を確認してください。" -ForegroundColor Red
    exit 1
}
$maxWait = 30
$waited = 0
while ($true) {
    $status = (docker inspect $dbContainer | ConvertFrom-Json).State.Health.Status
    if ($status -eq "healthy") { break }
    if ($waited -ge $maxWait) {
        Write-Host "DBが $maxWait 秒以内にhealthyになりませんでした。docker compose logs db を確認してください。" -ForegroundColor Red
        exit 1
    }
    Start-Sleep -Seconds 1
    $waited++
}
Write-Host "DB起動確認OK" -ForegroundColor Green

Write-Host "[2/4] DBの移行を実行中..." -ForegroundColor Cyan
Push-Location "$root\server"
try {
    go run ./cmd/migrate
    if ($LASTEXITCODE -ne 0) {
        Write-Host "DBの移行に失敗しました。server\.env の DATABASE_URL を確認してください。" -ForegroundColor Red
        exit 1
    }
} finally {
    Pop-Location
}

Write-Host "[3/4] Go APIを別ウィンドウで起動中..." -ForegroundColor Cyan
Start-Process powershell -ArgumentList @(
    "-NoExit", "-Command",
    "cd '$root\server'; go run ./cmd/api"
)

Write-Host "[4/4] フロントエンドを別ウィンドウで起動中..." -ForegroundColor Cyan
Start-Process powershell -ArgumentList @(
    "-NoExit", "-Command",
    "cd '$root\frontend'; npm run dev"
)

Start-Sleep -Seconds 3
Write-Host "起動コマンドを実行しました。数秒後に以下を確認できます:" -ForegroundColor Green
Write-Host "  フロントエンド: http://localhost:5173"
Write-Host "  API の稼働確認: http://127.0.0.1:8001/health/ready"

Start-Process "http://localhost:5173"
