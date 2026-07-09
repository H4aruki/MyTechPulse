#!/bin/sh
set -e

# DB は compose の healthcheck で起動待ち済み。テーブルを冪等に作成してから起動する
python init_db.py

exec uvicorn app.main:app --host 0.0.0.0 --port 8000
