# app/utils/security.py

import jwt
from datetime import datetime, timedelta, timezone
from ..config import settings

ALGORITHM = "HS256"

def create_access_token(user_id: int, username: str) -> str:
    """ログイン/サインアップ成功時にJWTを発行する"""
    expire = datetime.now(timezone.utc) + timedelta(minutes=settings.ACCESS_TOKEN_EXPIRE_MINUTES)
    payload = {"sub": username, "user_id": user_id, "exp": expire}
    return jwt.encode(payload, settings.SECRET_KEY, algorithm=ALGORITHM)

def decode_access_token(token: str) -> dict | None:
    """トークンを検証し、payloadを返す。不正・期限切れの場合はNoneを返す"""
    try:
        return jwt.decode(token, settings.SECRET_KEY, algorithms=[ALGORITHM])
    except jwt.PyJWTError:
        return None
