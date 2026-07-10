from pathlib import Path

from pydantic_settings import BaseSettings, SettingsConfigDict

# backend/.env を常に指す絶対パス（config.py は backend/app/ にある）。
# これでリポジトリルートから uvicorn を起動しても .env を読める。
# Docker では OS 環境変数が .env より優先されるため、このパスは無害。
ENV_PATH = Path(__file__).resolve().parent.parent / ".env"

class Settings(BaseSettings):
    # .envファイルからDATABASE_URLを読み込む
    DATABASE_URL: str
    QIITA_ACCESS_TOKEN: str
    # SQLAlchemyのSQLログ出力。デフォルトOFF、開発時のみ.envで true にする
    DB_ECHO: bool = False
    # JWT署名鍵。.envで必須指定（未設定時は起動失敗させて設定漏れに気付けるようにする）
    SECRET_KEY: str
    ACCESS_TOKEN_EXPIRE_MINUTES: int = 60 * 24

    # .envファイルのパスを指定
    model_config = SettingsConfigDict(env_file=str(ENV_PATH))

# 設定クラスのインスタンスを作成
settings = Settings()