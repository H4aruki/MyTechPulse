# app/routes/news.py

from fastapi import APIRouter, Depends
from sqlalchemy.orm import Session
from .. import schemas
from ..services import news_service
from ..database import get_db

router = APIRouter(prefix="/news", tags=["News"])

@router.post("/personal_news", response_model=schemas.article.PersonalNewsResponse)
async def get_personal_news(request: schemas.article.PersonalNewsRequest, db: Session = Depends(get_db)):
    # サービスから辞書形式でデータを受け取る
    article_data = await news_service.get_personalized_articles(db, username=request.username)
    
    # 辞書の中身が空かどうかで成功ステータスを判断
    if not article_data["qiita"] and not article_data["zenn"]:
        return {"status": 0, "qiita_articles": [], "zenn_articles": []}
    
    # 新しいレスポンス形式で返す
    return {
        "status": 1, 
        "qiita_articles": article_data["qiita"], 
        "zenn_articles": article_data["zenn"]
    }