# app/schemas/auth.py

from pydantic import BaseModel, ConfigDict
from typing import List


"""フロントエンド -> バックエンド"""

# login_check() のリクエストボディ
# ログイン時に { "username": "...", "password": "..." } という形のJSONを受け取ることを定義
class LoginRequest(BaseModel):
    username: str
    password: str

# create_user() のリクエストボディ
class UserCreateRequest(BaseModel):
    newusername: str
    newpassword: str
    favoritetags: List[str]



"""バックエンド -> フロントエンド"""

# login_check() のレスポンスボディ
class LoginResponse(BaseModel):
    status: int

# create_user() のレスポンスボディ
class CreateUserResponse(BaseModel):
    status: int



"""DBモデルから変換するためのスキーマ"""
# プログラム内部でデータを変換するために使う

class User(BaseModel):
    user_ID: int
    user_name: str

    model_config = ConfigDict(from_attributes=True)