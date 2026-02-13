# app/routes/click.py
from fastapi import APIRouter, Depends
from sqlalchemy.orm import Session
from .. import schemas
from ..services import click_service
from ..database import get_db

router = APIRouter(prefix="/article", tags=["Article"])

@router.post("/click", response_model=schemas.article.ClickArticleResponse)
def click_article(request: schemas.article.ClickArticleRequest, db: Session = Depends(get_db)):
    success = click_service.update_user_weights(db, username=request.username, clicked_tags=request.tags)
    return {"status": 1 if success else 0}