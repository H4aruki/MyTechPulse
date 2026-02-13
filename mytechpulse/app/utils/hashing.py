# app/utils/hashing.py

from passlib.context import CryptContext

# ハッシュ化のアルゴリズムとしてbcryptを指定し、コンテキストを作成
pwd_context = CryptContext(schemes=["bcrypt"], deprecated="auto")

class Hasher:
    @staticmethod
    def verify_password(plain_password: str, hashed_password: str) -> bool:
        return pwd_context.verify(plain_password, hashed_password)   #平文パスワードとハッシュ化済みパスワードが正しいか検証

    @staticmethod
    def get_password_hash(password: str) -> str:
        return pwd_context.hash(password)   #平文パスワードをハッシュ化