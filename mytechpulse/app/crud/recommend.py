# app/crud/recommend.py
#recommend（中間）テーブルに対する操作をまとめたファイル

from sqlalchemy.orm import Session
from .. import models

# Create（作成） 操作
def create_recommendation(db: Session, user_id: int, tag_id: int, match_int: int = 1):
    """ユーザーとタグの関連を作成する (match_intの初期値は仮で1に設定)"""
    db_recommend = models.Recommend(
        user_ID=user_id,
        tag_ID=tag_id,
        match_int=match_int
    )
    db.add(db_recommend)



def get_recommendations_by_user_id(db: Session, user_id: int) -> list[models.Recommend]:
    """特定のユーザーの全てのタグの重みを取得する"""
    return db.query(models.Recommend).filter(models.Recommend.user_ID == user_id).all()

def get_recommendation_by_user_and_tag(db: Session, user_id: int, tag_id: int) -> models.Recommend | None:
    """特定のユーザーとタグの組み合わせのレコードを取得する"""
    return db.query(models.Recommend).filter(
        models.Recommend.user_ID == user_id,
        models.Recommend.user_ID == tag_id
    ).first()

#計算した小数に10000を掛けて整数として保存し、読み出す際に10000で割るという運用を想定
def update_or_create_recommendation(db: Session, user_id: int, tag_id: int, new_weight: int):
    """ユーザーのタグの重みを更新、または新規作成する"""
    recommendation = get_recommendation_by_user_and_tag(db, user_id, tag_id)
    if recommendation:
        recommendation.match_int = new_weight
    else:
        new_recommendation = models.Recommend(user_ID=user_id, tag_ID=tag_id, match_int=new_weight)
        db.add(new_recommendation)