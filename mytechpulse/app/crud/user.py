# app/crud/user.py

from sqlalchemy.orm import Session
from .. import models

def get_user_by_username(db: Session, username: str) -> models.User | None:
    """
    ユーザー名でユーザー情報を取得する。
    存在しない場合は None を返す。
    """
    return db.query(models.User).filter(models.User.user_name == username).first()

def create_user(db: Session, user_name: str, password: str) -> models.User:
    """
    新しいユーザーを作成する。
    注意: この関数はパスワードをハッシュ化しません。
    サービス層でハッシュ化したパスワードを受け取ることを想定しています。
    """
    # SQLAlchemyモデルのインスタンスを作成
    db_user = models.User(
        user_name=user_name,
        password=password  # services層から渡されたハッシュ化済みパスワード
    )
    
    # データベースセッションに追加（コミットはしない）
    db.add(db_user)
    
    return db_user