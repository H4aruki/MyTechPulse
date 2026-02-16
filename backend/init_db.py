import mysql.connector
from app.config import settings

def init_database():
    # 1. データベース自体の作成
    # URLから接続情報を分解（mysql+mysqlconnector://user@host/db）
    # シンプルにroot/パスワードなし/localhostで接続試行
    try:
        conn = mysql.connector.connect(
            host="localhost",
            user="root",
            password=""
        )
        cursor = conn.cursor()
        
        # データベース作成 (image_8a0451.png に基づき 'mytechpulse' を使用)
        db_name = "mytechpulse" 
        cursor.execute(f"CREATE DATABASE IF NOT EXISTS {db_name} CHARACTER SET utf8mb4")
        print(f"Database '{db_name}' ready.")
        
        cursor.close()
        conn.close()

        # 2. テーブルの作成 (SQLAlchemyのモデルから自動生成)
        # 既存のmodelsで定義されている構造を流し込む
        from app.database import engine
        from app.models import Base, User, Tag, Recommend  # モデルのベースクラスをインポート
        
        Base.metadata.create_all(bind=engine)
        print("Tables created successfully.")
        
    except Exception as e:
        print(f"Error: {e}")

if __name__ == "__main__":
    init_database()