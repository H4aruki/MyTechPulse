# app/services/auth_service.py

from sqlalchemy.orm import Session
from .. import crud, schemas
from ..utils.hashing import Hasher


"""ログイン処理"""
def login_check_service(db: Session, request: schemas.auth.LoginRequest) -> int:
    """login_checkのロジックを処理し、ステータスコードを返す"""
    user = crud.user.get_user_by_username(db, username=request.username)
    
    if not user:
        return 0 # ユーザー名がない
    
    if not Hasher.verify_password(request.password, user.password):
        return 2 # パスワードが不一致
        
    return 1 # ログイン成功


"""ユーザー登録処理"""
def create_user_service(db: Session, request: schemas.auth.UserCreateRequest) -> int:
    """create_userのロジックを処理し、ステータスコードを返す"""
    # 既に同じユーザー名が存在するかチェック
    existing_user = crud.user.get_user_by_username(db, username=request.newusername)
    if existing_user:
        return 0 # 登録失敗 (ユーザー名が重複)

    # 複数のテーブルを書き換えるため、トランザクション処理を行う
    try:
        # 1. ユーザーをDBに作成
        hashed_password = Hasher.get_password_hash(request.newpassword)
        db_user = crud.user.create_user(db=db, user_name=request.newusername, password=hashed_password)
        db.flush() # これにより db_user.user_ID が確定する

        # 2. タグを処理
        for tag_name in request.favoritetags:
            db_tag = crud.tag.get_tag_by_name(db, tag_name=tag_name)
            if not db_tag:
                # 存在しないタグは新規作成
                db_tag = crud.tag.create_tag(db, tag_name=tag_name)
                db.flush() # これにより db_tag.tag_ID が確定する
            
            # 3. recommendテーブルに関連を作成
            crud.recommend.create_recommendation(db, user_id=db_user.user_ID, tag_id=db_tag.tag_ID)

        db.commit() # 全ての処理が成功したらコミット
        return 1 # 登録成功

    except Exception as e:
        print(f"Error: {e}") # エラーログ
        db.rollback() # エラーが発生したらロールバック
        return 0 # 登録失敗