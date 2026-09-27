import os
from pathlib import Path

os.environ["MTP_DISABLE_DOTENV"] = "1"
os.environ["DATABASE_URL"] = (
    "postgresql+psycopg://postgres:postgres@127.0.0.1:5432/mytechpulse_test"
)
os.environ["QIITA_ACCESS_TOKEN"] = "synthetic-qiita-token"
os.environ["SECRET_KEY"] = "synthetic-test-key-with-at-least-32-bytes"
os.environ["DB_ECHO"] = "false"
os.environ["CORS_ALLOWED_ORIGINS"] = "http://localhost:5173"

FIXTURE_DIR = Path(__file__).parents[2] / "testdata" / "compatibility"


def fixture_path(name: str) -> Path:
    return FIXTURE_DIR / name
