import httpx
import asyncio
from urllib.parse import quote
from datetime import datetime, timedelta, timezone
from sqlalchemy.orm import Session
from .. import crud, models, schemas
from ..config import settings

# Zennの特定タグの記事を取得する非同期関数
async def fetch_zenn_articles_for_tag(client: httpx.AsyncClient, tag: str) -> list:
    url = "https://zenn.dev/api/articles"
    # Zennのtopicnameは小文字でないとヒットしない（実機確認済み: topicname=Pythonは0件、topicname=pythonは48件）
    params = {"topicname": tag.lower(), "count": 5}
    try:
        response = await client.get(url, params=params)
        response.raise_for_status()
        return response.json().get("articles", [])
    except httpx.HTTPError as e:
        print(f"Error fetching Zenn API for tag {tag}: {e}")
        return []

# Qiitaの特定タグの記事を取得する非同期関数
async def fetch_qiita_articles_for_tag(client: httpx.AsyncClient, tag: str):
    # タグ値はDB由来だがURLパスへの埋め込みなので必ずエンコードする。
    # safe=""で「/」もエンコードし、Sass/SCSSのようなタグや「../」を含む値が
    # パス区切りとして解釈されて別エンドポイントに到達するのを防ぐ（S6）
    url = f"https://qiita.com/api/v2/tags/{quote(tag, safe='')}/items"
    headers = {"Authorization": f"Bearer {settings.QIITA_ACCESS_TOKEN}"}
    params = {"page": 1, "per_page": 20}
    try:
        response = await client.get(url, headers=headers, params=params)
        response.raise_for_status()
        return response.json()
    except httpx.HTTPError as e:
        print(f"Error fetching Qiita API for tag {tag}: {e}")
        return []

# Zennの検索結果からArticleを組み立てる。一覧APIには記事本来のtopicsが無いため、
# どのtopicname検索でヒットしたかをtagsとして注入し、複数タグでヒットした記事はマージする
def _build_zenn_articles_by_url(top_tags, zenn_results, since=None):
    articles_by_url = {}
    for tag, articles in zip(top_tags, zenn_results):
        for article in articles:
            if since is not None:
                created_at = datetime.fromisoformat(article["published_at"].replace("Z", "+00:00"))
                if created_at <= since:
                    continue
            article_url = f"https://zenn.dev{article['path']}"
            if article_url in articles_by_url:
                existing = articles_by_url[article_url]
                if tag not in existing.tags:
                    existing.tags = existing.tags + [tag]
            else:
                articles_by_url[article_url] = schemas.article.Article(
                    title=article.get("title", ""),
                    url=article_url,
                    source="Zenn",
                    tags=[tag],
                    likes=article.get("liked_count", 0),
                    published_at=article.get("published_at", "").split("T")[0]
                )
    return articles_by_url

# メインのサービス関数
async def get_personalized_articles(db: Session, user: models.User) -> dict:
    # 1. ユーザーのタグの重みを取得
    recommends = crud.recommend.get_recommendations_by_user_id(db, user_id=user.user_ID)
    user_weights = {rec.tag.tag_name.lower(): (rec.match_int / 10000.0) for rec in recommends}
    if not user_weights: 
        return {"qiita": [], "zenn": []}

    # 2. ZennとQiitaから同時に記事を取得（ユーザーの上位5タグでそれぞれタグ別検索）
    user_original_tags = {rec.tag.tag_name: (rec.match_int / 10000.0) for rec in recommends}
    top_tags = sorted(user_original_tags.keys(), key=lambda t: user_original_tags[t], reverse=True)[:5]

    async with httpx.AsyncClient() as client:
        zenn_tasks = [fetch_zenn_articles_for_tag(client, tag) for tag in top_tags]
        qiita_tasks = [fetch_qiita_articles_for_tag(client, tag) for tag in top_tags]
        zenn_results, qiita_results = await asyncio.gather(
            asyncio.gather(*zenn_tasks),
            asyncio.gather(*qiita_tasks),
        )

    # 3. 取得した全記事を共通の形式に整形・統合
    all_articles = {}
    five_days_ago = datetime.now(timezone.utc) - timedelta(days=5)
    two_weeks_ago = datetime.now(timezone.utc) - timedelta(days=14)

    # Zennの記事を処理（直近2週間以内、タグは検索クエリ由来のためQiitaより緩い期間で候補を確保する）
    zenn_by_url = _build_zenn_articles_by_url(top_tags, zenn_results, since=two_weeks_ago)
    all_articles.update(zenn_by_url)

    # Qiitaの記事を処理
    for article_list in qiita_results:
        for article in article_list:
            article_tags_lower = [tag["name"].lower() for tag in article.get("tags", [])]

            if any(tag in user_weights for tag in article_tags_lower):
                created_at = datetime.fromisoformat(article["created_at"])
                if created_at > five_days_ago:
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
    # Zennは取得時点で既にZenn独自のトレンド順に絞り込み済みのため、likesは使わず
    # タグ重み合計のみでスコアリングする（Qiitaは従来通りlikesも加味する）
    scored_articles = []
    for article in all_articles.values():
        tag_weight_sum = sum(user_weights.get(tag.lower(), 0) for tag in article.tags)
        if article.source == "Zenn":
            score = tag_weight_sum
        else:
            score = tag_weight_sum * (article.likes + 1)
        scored_articles.append({"score": score, "data": article})

    # 5. QiitaとZennに分離し、それぞれスコア順にソートする
    qiita_sorted = [
        item["data"] for item in sorted(
            (item for item in scored_articles if item["data"].source == "Qiita"),
            key=lambda item: item["score"],
            reverse=True
        )
    ]
    zenn_sorted = [
        item["data"] for item in sorted(
            (item for item in scored_articles if item["data"].source == "Zenn"),
            key=lambda item: item["score"],
            reverse=True
        )
    ]
    
    # フォールバック処理: 上位5タグいずれのZenn検索でも直近2週間以内に該当記事が無かった場合、
    # 追加のAPI呼び出しはせず、期間フィルタだけ撤廃して同じ検索結果から補う
    if not zenn_sorted:
        fallback_by_url = _build_zenn_articles_by_url(top_tags, zenn_results)
        zenn_sorted = sorted(
            fallback_by_url.values(),
            key=lambda article: sum(user_weights.get(tag.lower(), 0) for tag in article.tags),
            reverse=True
        )

    # それぞれ上位10件ずつを辞書で返す
    return {
        "qiita": qiita_sorted[:10],
        "zenn": zenn_sorted[:10]
    }