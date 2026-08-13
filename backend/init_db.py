def init_database():
    """モデル定義からテーブルを冪等に作成する。

    データベース自体の作成は行わない。PostgreSQL には MySQL の
    ``CREATE DATABASE IF NOT EXISTS`` に相当する構文が無く、
    docker-compose.yml の ``POSTGRES_DB`` がコンテナ初回起動時に
    ``mytechpulse`` を作るため、こちら側で面倒を見る必要がない。

    DBの起動待ちも compose の healthcheck (``pg_isready``) と
    ``depends_on: service_healthy`` が担保しているのでリトライは持たない。
    """
    try:
        from app.database import engine
        from app.models import Base, User, Tag, Recommend  # noqa: F401

        Base.metadata.create_all(bind=engine)
        print("Tables created successfully.")

    except Exception as e:
        print(f"Error: {e}")
        raise


if __name__ == "__main__":
    init_database()
