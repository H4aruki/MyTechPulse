# app/utils/scoring.py
from typing import Dict

# 忘却率 (設定ファイルに移すのが望ましい)
ALPHA = 0.8

def calculate_new_weights(current_weights: Dict[str, float], clicked_tags: list) -> Dict[str, float]:
    """
    現在の重みとクリックされたタグを基に、指数的減衰を適用して新しい重みを計算する。
    
    Args:
        current_weights: 現在のタグの重みを持つ辞書 (例: {"Python": 0.5, "SQL": 0.3})
        clicked_tags: クリックされた記事に含まれるタグのリスト (例: ["Python", "FastAPI"])
        
    Returns:
        更新後のタグの重みを持つ辞書
    """
    # 1. 全ての既存の重みを減衰させる
    decayed_weights = {tag: weight * ALPHA for tag, weight in current_weights.items()}
    
    # 2. クリックされたタグの重みを加算（強化）する
    for tag in clicked_tags:
        current_val = decayed_weights.get(tag, 0)
        decayed_weights[tag] = current_val + (1 - ALPHA)
        
    return decayed_weights