# app/crud/tag.py

from sqlalchemy.orm import Session
from .. import models

# タグ名でタグを取得する。（Readの部分）
def get_tag_by_name(db: Session, tag_name: str) -> models.Tag | None:   #database.pyのget_db関数から受け取るdbを引数に取る
    """タグ名でタグを取得する"""
    return db.query(models.Tag).filter(models.Tag.tag_name == tag_name).first()   #条件に一致した最初の1件だけを取得。もし該当するものがなければNone


# 新しいタグを作成する。（Createの部分）
#app/models/tag.pyで定義したTagクラスのインスタンス（新しいオブジェクト）を作成
def create_tag(db: Session, tag_name: str) -> models.Tag:
    """新しいタグを作成する"""
    db_tag = models.Tag(tag_name=tag_name)
    db.add(db_tag)
    # ここでは一旦commitせず、サービス層でまとめてcommitする
    #実際の保存確定は、これらの部品を呼び出すservices層で行う
    return db_tag