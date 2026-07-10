# app/routes/click.py
from fastapi import APIRouter, Depends
from sqlalchemy.orm import Session
from .. import models, schemas
from ..services import click_service
from ..database import get_db
from ..dependencies import get_current_user

router = APIRouter(prefix="/article", tags=["Article"])

@router.post("/click", response_model=schemas.article.ClickArticleResponse)
def click_article(
    request: schemas.article.ClickArticleRequest,
    db: Session = Depends(get_db),
    current_user: models.User = Depends(get_current_user),
):
    success = click_service.update_user_weights(db, user=current_user, clicked_tags=request.tags)
    return {"status": 1 if success else 0}