from sqlalchemy import create_engine
from sqlalchemy.exc import OperationalError

# データベース接続情報（pytestではなく手動実行の接続確認スクリプト）
# フォーマット: "postgresql+psycopg://<user>:<password>@<host>:<port>/<database>"
# 事前に `docker compose up -d db` でDBを起動しておくこと
db_url = "postgresql+psycopg://postgres:postgres@localhost:5432/mytechpulse"

try:
    # データベースエンジンを作成
    engine = create_engine(db_url)

    # 接続を試みる
    connection = engine.connect()

    print("✅ データベースへの接続に成功しました！")

    # 接続を閉じる
    connection.close()

except OperationalError as e:
    print(f"❌ 接続に失敗しました。エラー: {e}")
except Exception as e:
    print(f"❌予期せぬエラーが発生しました: {e}")