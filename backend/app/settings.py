from pathlib import Path

from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    DATABASE_URL: str
    QIITA_ACCESS_TOKEN: str
    DB_ECHO: bool = False
    SECRET_KEY: str
    ACCESS_TOKEN_EXPIRE_MINUTES: int = 60 * 24
    CORS_ALLOWED_ORIGINS: str = ""

    model_config = SettingsConfigDict()


def load_settings(*, dotenv_path: Path | None) -> Settings:
    return Settings(_env_file=dotenv_path)
