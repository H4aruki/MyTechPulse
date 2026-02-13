import httpx
import asyncio
from datetime import datetime, timedelta
from sqlalchemy.orm import Session
from .. import crud, schemas
from ..config import settings

# Zennの最新記事を複数ページ分、並行して取得する非同期関数
async def fetch_zenn_articles(client: httpx.AsyncClient, num_pages: int = 3):
    tasks = []
    for page in range(1, num_pages + 1):
        task = client.get(f"https://zenn.dev/api/articles?page={page}")
        tasks.append(task)
    
    try:
        responses = await asyncio.gather(*tasks)
        all_articles = []
        for response in responses:
            response.raise_for_status()
            all_articles.extend(response.json().get("articles", []))
        return all_articles
        
    except httpx.RequestError as e:
        print(f"Error fetching Zenn API: {e}")
        return []

# Qiitaの特定タグの記事を取得する非同期関数
async def fetch_qiita_articles_for_tag(client: httpx.AsyncClient, tag: str):
    url = f"https://qiita.com/api/v2/tags/{tag}/items"
    headers = {"Authorization": f"Bearer {settings.QIITA_ACCESS_TOKEN}"}
    params = {"page": 1, "per_page": 20}
    try:
        response = await client.get(url, headers=headers, params=params)
        response.raise_for_status()
        return response.json()
    except httpx.RequestError as e:
        print(f"Error fetching Qiita API for tag {tag}: {e}")
        return []

# メインのサービス関数
async def get_personalized_articles(db: Session, username: str) -> dict:
    # 1. ユーザーのタグの重みを取得
    user = crud.user.get_user_by_username(db, username=username)
    if not user: 
        return {"qiita": [], "zenn": []}
    
    recommends = crud.recommend.get_recommendations_by_user_id(db, user_id=user.user_ID)
    user_weights = {rec.tag.tag_name.lower(): (rec.match_int / 10000.0) for rec in recommends}
    if not user_weights: 
        return {"qiita": [], "zenn": []}

    # 2. ZennとQiitaから同時に記事を取得
    user_original_tags = {rec.tag.tag_name: (rec.match_int / 10000.0) for rec in recommends}
    top_tags = sorted(user_original_tags.keys(), key=lambda t: user_original_tags[t], reverse=True)[:5]
    
    async with httpx.AsyncClient() as client:
        tasks = [fetch_zenn_articles(client)]
        for tag in top_tags:
            tasks.append(fetch_qiita_articles_for_tag(client, tag))
        
        results = await asyncio.gather(*tasks)

    # 3. 取得した全記事を共通の形式に整形・統合
    all_articles = {}
    five_days_ago = datetime.now() - timedelta(days=5)

    # Zennの記事を処理 (生データはここで保持)
    zenn_articles = results[0]
    for article in zenn_articles:
        article_tags_lower = [topic['name'].lower() for topic in article.get("topics", [])]
        
        if any(tag in user_weights for tag in article_tags_lower):
            created_at = datetime.fromisoformat(article["published_at"].replace("Z", ""))
            if created_at.replace(tzinfo=None) > five_days_ago:
                article_url = f"https://zenn.dev{article['path']}"
                all_articles[article_url] = schemas.article.Article(
                    title=article.get("title", ""),
                    url=article_url,
                    source="Zenn",
                    tags=[topic['name'] for topic in article.get("topics", [])],
                    likes=article.get("liked_count", 0),
                    published_at=article.get("published_at", "").split("T")[0]
                )

    # Qiitaの記事を処理
    qiita_results = results[1:]
    for article_list in qiita_results:
        for article in article_list:
            article_tags_lower = [tag["name"].lower() for tag in article.get("tags", [])]

            if any(tag in user_weights for tag in article_tags_lower):
                created_at = datetime.fromisoformat(article["created_at"])
                if created_at.replace(tzinfo=None) > five_days_ago:
                    if article["url"] not in all_articles:
                        all_articles[article["url"]] = schemas.article.Article(
                            title=article.get("title", ""),
                            url=article.get("url", ""),
                            source="Qiita",
                            tags=[tag["name"] for tag in article.get("tags", [])],
                            likes=article.get("likes_count", 0),
                            published_at=article.get("created_at", "").split("T")[0]
                        )

    # 4. 統合した記事リストをスコアリング
    scored_articles = []
    for article in all_articles.values():
        score = sum(user_weights.get(tag.lower(), 0) for tag in article.tags)
        score *= (article.likes + 1)
        scored_articles.append({"score": score, "data": article})

    # 5. QiitaとZennに分離し、それぞれスコア順にソートする
    qiita_sorted = sorted(
        [item["data"] for item in scored_articles if item["data"].source == "Qiita"], 
        key=lambda x: next((s["score"] for s in scored_articles if s["data"] == x), 0), 
        reverse=True
    )
    zenn_sorted = sorted(
        [item["data"] for item in scored_articles if item["data"].source == "Zenn"],
        key=lambda x: next((s["score"] for s in scored_articles if s["data"] == x), 0),
        reverse=True
    )
    
    # フォールバック処理: もしフィルタリング後のZenn記事が0件だった場合
    if not zenn_sorted:
        fallback_zenn = []
        # 元のZenn記事リストから、日付フィルターのみを適用して上位10件を取得
        for article in zenn_articles:
            created_at = datetime.fromisoformat(article["published_at"].replace("Z", ""))
            if created_at.replace(tzinfo=None) > five_days_ago:
                article_url = f"https://zenn.dev{article['path']}"
                fallback_zenn.append(schemas.article.Article(
                    title=article.get("title", ""),
                    url=article_url,
                    source="Zenn",
                    tags=[topic['name'] for topic in article.get("topics", [])],
                    likes=article.get("liked_count", 0),
                    published_at=article.get("published_at", "").split("T")[0]
                ))
            if len(fallback_zenn) >= 10:
                break 
        zenn_sorted = fallback_zenn

    # それぞれ上位10件ずつを辞書で返す
    return {
        "qiita": qiita_sorted[:10],
        "zenn": zenn_sorted[:10]
    }