FROM python:3.12-slim

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1

WORKDIR /app

# ビルドコンテキストはリポジトリルート。requirements を先に入れてレイヤキャッシュを効かせる
COPY requirements.txt /app/requirements.txt
RUN pip install --no-cache-dir --upgrade pip \
 && pip install --no-cache-dir -r requirements.txt

# backend/ の中身（app/ パッケージ, init_db.py, entrypoint.sh 等）を /app に配置
COPY backend/ /app/

RUN chmod +x /app/entrypoint.sh

EXPOSE 8000
ENTRYPOINT ["/app/entrypoint.sh"]
