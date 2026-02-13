from sqlalchemy import create_engine
from sqlalchemy.exc import OperationalError

# データベース接続情報
# フォーマット: "mysql+mysqlconnector://<user>:<password>@<host>:<port>/<database>"
db_url = "mysql+mysqlconnector://root@localhost:3306/mydatabase"

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