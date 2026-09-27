from pathlib import Path

import os

from .settings import load_settings

# backend/.env を指す絶対パス（config.py は backend/app/ にある）。
# 通常起動では従来どおりこのファイルを読み、テスト時だけ読み込みを止める。
ENV_PATH = Path(__file__).resolve().parent.parent / ".env"

dotenv_path = None if os.environ.get("MTP_DISABLE_DOTENV") == "1" else ENV_PATH
settings = load_settings(dotenv_path=dotenv_path)
