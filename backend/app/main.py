# app/main.py

from fastapi import FastAPI
from .routes import auth , news, click   # auth , news, clickルーターをインポート
from fastapi.middleware.cors import CORSMiddleware
from .config import settings

app = FastAPI()

origins = [
    "http://localhost:3000",
    "http://localhost:5500",
    "http://127.0.0.1:5500",
    "http://127.0.0.1:8000", # フロントエンドの開発サーバーのURL
]
# 本番等の追加オリジンは .env の CORS_ALLOWED_ORIGINS（カンマ区切り）で指定する
origins += [o.strip() for o in settings.CORS_ALLOWED_ORIGINS.split(",") if o.strip()]

app.add_middleware(
    CORSMiddleware,
    allow_origins=origins,
    # JWTはAuthorizationヘッダーで渡しCookieは使わないため、credentialsは許可しない
    allow_credentials=False,
    allow_methods=["*"], # 全てのHTTPメソッドを許可
    allow_headers=["*"], # 全てのHTTPヘッダーを許可
)

# /auth で始まるパスへのリクエストをauth.routerに転送
app.include_router(auth.router)
app.include_router(news.router)   # newsルーターを登録
app.include_router(click.router)  # clickルーターを登録

@app.get("/")
def read_root():
    return {"Hello": "World"}