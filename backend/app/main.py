# app/main.py

from fastapi import FastAPI
from .routes import auth , news, click   # auth , news, clickルーターをインポート
from fastapi.middleware.cors import CORSMiddleware

app = FastAPI()

origins = [
    "http://localhost:3000", # フロントエンドの開発サーバーのURL
    # 将来的には本番環境のフロントエンドのURLも追加する
    # "https://www.your-production-site.com",
]

app.add_middleware(
    CORSMiddleware,
    allow_origins=origins,
    allow_credentials=True,
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