# ローカル開発環境を1コマンドで起動する。
# db(docker) → backend(uvicorn) → frontend(vite) の順に立ち上げ、
# backendとfrontendはそれぞれ別ウィンドウで起動してログを分離する。
#
# 使い方: .\dev.ps1

$ErrorActionPreference = "Stop"
$root = $PSScriptRoot

if (-not (Test-Path "$root\backend\.env")) {
    Write-Host "backend\.env が見つかりません。backend\.env.example をコピーして作成してください。" -ForegroundColor Red
    exit 1
}

Write-Host "[1/3] DBコンテナを起動中..." -ForegroundColor Cyan
docker compose -f "$root\docker-compose.yml" up -d db
if ($LASTEXITCODE -ne 0) {
    Write-Host "docker compose up に失敗しました。Docker Desktopが起動しているか確認してください。" -ForegroundColor Red
    exit 1
}

Write-Host "[1/3] DBのヘルスチェック待ち..." -ForegroundColor Cyan
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

Write-Host "[2/3] バックエンドを別ウィンドウで起動中..." -ForegroundColor Cyan
Start-Process powershell -ArgumentList @(
    "-NoExit", "-Command",
    "cd '$root\backend'; .\venv\Scripts\python.exe -m uvicorn app.main:app --reload"
)

Write-Host "[3/3] フロントエンドを別ウィンドウで起動中..." -ForegroundColor Cyan
Start-Process powershell -ArgumentList @(
    "-NoExit", "-Command",
    "cd '$root\frontend'; npm run dev"
)

Start-Sleep -Seconds 3
Write-Host "起動コマンドを実行しました。数秒後に以下を確認できます:" -ForegroundColor Green
Write-Host "  フロントエンド: http://localhost:5173"
Write-Host "  API Swagger UI: http://127.0.0.1:8000/docs"

Start-Process "http://localhost:5173"
Start-Process "http://127.0.0.1:8000/docs"
