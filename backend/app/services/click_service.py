# app/services/click_service.py
from sqlalchemy.orm import Session
from .. import crud
from ..utils import scoring # scoring.pyをインポート

def update_user_weights(db: Session, username: str, clicked_tags: list) -> bool:
    user = crud.user.get_user_by_username(db, username=username)
    if not user:
        return False

    try:
        # 1. 現在の重みをDBから取得
        recommends = crud.recommend.get_recommendations_by_user_id(db, user_id=user.user_ID)
        # DBの整数から計算用の小数に変換
        current_weights_float = {rec.tag.tag_name: (rec.match_int / 10000.0) for rec in recommends}
        
        # 2. utils/scoring.pyの関数を呼び出して新しい重みを計算
        new_weights = scoring.calculate_new_weights(current_weights_float, clicked_tags)
        
        # 3. 計算結果をDBに保存
        for tag_name, weight in new_weights.items():
            db_tag = crud.tag.get_tag_by_name(db, tag_name)
            if not db_tag:
                db_tag = crud.tag.create_tag(db, tag_name)
                db.flush() # 新しいタグのIDを確定させる
            
            # 計算用の小数をDB保存用の整数に変換
            int_weight = int(weight * 10000)
            crud.recommend.update_or_create_recommendation(
                db, user_id=user.user_ID, tag_id=db_tag.tag_ID, new_weight=int_weight
            )
        
        db.commit()
        return True
    except Exception as e:
        print(f"Error updating weights: {e}")
        db.rollback()
        return False