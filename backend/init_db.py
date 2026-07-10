import re
import time

import pymysql
from sqlalchemy.engine import make_url

from app.config import settings

_VALID_DB_NAME = re.compile(r"^[A-Za-z0-9_]+$")


def _connect_server(url, max_retries: int = 10, wait_seconds: float = 2.0):
    """DATABASE_URL からホスト/ユーザー/パスワード/ポートを取り出し、
    DB名を指定せずにMySQLサーバーへ接続する。起動直後のMySQLに備えてリトライする。
    Docker(host=db)/ローカル(host=localhost)のどちらでも同じコードで動く。"""
    last_error = None
    for attempt in range(1, max_retries + 1):
        try:
            return pymysql.connect(
                host=url.host or "localhost",
                port=url.port or 3306,
                user=url.username or "root",
                password=url.password or "",
                charset="utf8mb4",
            )
        except pymysql.MySQLError as e:
            last_error = e
            print(f"MySQL not ready (attempt {attempt}/{max_retries}): {e}")
            time.sleep(wait_seconds)
    raise last_error


def init_database():
    try:
        # 1. DATABASE_URL から接続情報を導出し、データベース自体を作成
        url = make_url(settings.DATABASE_URL)
        db_name = url.database or "mytechpulse"
        if not _VALID_DB_NAME.match(db_name):
            raise ValueError(
                f"Invalid database name in DATABASE_URL: {db_name!r} "
                "(allowed: letters, digits, underscore)"
            )

        conn = _connect_server(url)
        try:
            cursor = conn.cursor()
            cursor.execute(
                f"CREATE DATABASE IF NOT EXISTS `{db_name}` CHARACTER SET utf8mb4"
            )
            conn.commit()
            print(f"Database '{db_name}' ready.")
            cursor.close()
        finally:
            conn.close()

        # 2. テーブルの作成 (SQLAlchemyのモデルから自動生成)
        from app.database import engine
        from app.models import Base, User, Tag, Recommend  # noqa: F401

        Base.metadata.create_all(bind=engine)
        print("Tables created successfully.")

    except Exception as e:
        print(f"Error: {e}")
        raise


if __name__ == "__main__":
    init_database()
