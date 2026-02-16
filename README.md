

<div align="center">
    <img src="./img/mytechpulse-logo3.png" width=400 height=400>
</div>

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
