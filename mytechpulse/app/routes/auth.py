# app/routes/auth.py

from fastapi import APIRouter, Depends, HTTPException
from sqlalchemy.orm import Session
from .. import schemas, services
from ..database import get_db
from .. import crud

router = APIRouter(
    prefix="/auth", # このファイル内のエンドポイントは全て /auth から始まる
    tags=["Auth"]   # APIドキュメントでグループ化される
)

@router.post("/login_check", response_model=schemas.auth.LoginResponse)
def login_check(request: schemas.auth.LoginRequest, db: Session = Depends(get_db)):
    status_code = services.auth_service.login_check_service(db, request)
    return {"status": status_code}

@router.post("/create_user", response_model=schemas.auth.CreateUserResponse)
def create_user(request: schemas.auth.UserCreateRequest, db: Session = Depends(get_db)):
    status_code = services.auth_service.create_user_service(db, request)
    if status_code == 0 and crud.user.get_user_by_username(db, username=request.newusername):
         raise HTTPException(status_code=409, detail="Username already registered")

    if status_code == 0:
        raise HTTPException(status_code=500, detail="Could not create user")

    return {"status": status_code}