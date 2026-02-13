from pydantic_settings import BaseSettings, SettingsConfigDict

class Settings(BaseSettings):
    # .envファイルからDATABASE_URLを読み込む
    DATABASE_URL: str
    QIITA_ACCESS_TOKEN: str
    
    # .envファイルのパスを指定
    model_config = SettingsConfigDict(env_file=".env")

# 設定クラスのインスタンスを作成
settings = Settings()