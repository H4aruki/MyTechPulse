# app/database.py
#FastAPIアプリケーションとMySQLデータベースを接続するための設定ファイル

from sqlalchemy import create_engine
from sqlalchemy.orm import sessionmaker
from .config import settings # 設定情報をインポート


#SQLAlchemyがデータベースと通信するための中心的な接続点を作成
# configから読み込んだDATABASE_URLを使ってengineを作成
engine = create_engine(
    settings.DATABASE_URL,   #configから読み込んだDATABASE_URLを使ってengineを作成
    echo=True   # 開発中はTrueにしておくとSQLログが見れて便利
)


# データベースセッションを作成するためのクラス
SessionLocal = sessionmaker(autocommit=False, autoflush=False, bind=engine)


# FastAPIのDI（依存性注入）システムで使用する関数
def get_db():
    db = SessionLocal()   #APIリクエストが来たタイミングで、データベースセッションを作成
    try:
        #作成したセッション db をAPIエンドポイントの関数（routesの中の関数）に一時的に貸し出す。APIの処理はこの貸し出された db を使って行われる。
        yield db   
    finally:   #APIの処理が成功しようがエラーで終わろうが、必ず最後に実行されるブロック
        db.close()   #APIリクエストが終わったタイミングで、データベースセッションを閉じる