
# 「MyTechPulse」ー私専用のTechニュースー

## Your daily does of tech：パーソナライズされた技術情報の躍動をあなたに。

MyTechPulseは、「色んなサイトがあって全部追いかけられない」、「サイトを巡回するのが面倒」といった悩みを解決するために生まれました。

<div origin= center>
    <img src="./img\Articles.png" width= "1000" height="800">
</div>

### What MyTechPulse does
複数の技術メディアやブログから情報を自動収集。あなたの過去の閲覧傾向や興味関心を分析し、膨大な情報の中から「あなたに最適化された技術の脈動」を届けます。
- **情報の集約**: 主要な技術サイトを巡回する手間をゼロに。
- **パーソナライズ**: 読めば読むほど、あなたの好みに合った記事が優先的に届きます。
- **効率的なキャッチアップ**: 隙間時間で、今知るべきトレンドを効率よく把握。

## SETUP
### 1. 事前準備
以下のツールがインストールされ、起動していることを確認して下さい。

・Python3.10以上

・XAMPP（コントロールパネルからMYSQLを「Start」しておいてください。）

### 2. 環境構築
1. [最新のZIPファイルをダウンロード](https://github.com/H4aruki/MyTechPulse/archive/refs/heads/main.zip) し、任意のフォルダに展開（解凍）してください。

2. 必要ライブラリのインストール
   ```bash
   pip instal -r requirements.txt
   ```
3. データベースの構築
   ```bash
   cd backend
   python init_db.py
   ```
### 3. システムの起動
   ```bash
   uvicorn app.main:app --reload
   ```
ブラウザで```index.html```を開くか、VSCodeのLive Serverで起動してください。

## Contributors ✨
<table>
    <tr>
        <td align="center">
            <a href=""https://github.com/H4aruki">
                <img src="https://github.com/H4aruki.png" width="100px;" alt=""/><br />
                <sub><b>H4aruki</b></sub>
            </a><br />
            <a href="https://github.com/H4aruki/MyTechPulse/commits?author=H4aruki" title="Code">💻</a>
            <a href="https://github.com/H4aruki/MyTechPulse/commits?author=H4aruki" title="Construction">🚧</a>
        </td>
    </tr>
</table>


