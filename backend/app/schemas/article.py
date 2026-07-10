# app/schemas/article.py

from pydantic import BaseModel
from typing import List

# --- Article Object Schema ---
class Article(BaseModel):
    title: str
    url: str
    source: str
    tags: List[str]
    likes: int
    published_at: str

# --- personal_news Schemas ---
class PersonalNewsResponse(BaseModel):
    status: int
    qiita_articles: List[Article]
    zenn_articles: List[Article]

# --- click_article Schemas ---
class ClickArticleRequest(BaseModel):
    tags: List[str]

class ClickArticleResponse(BaseModel):
    status: int