# app/models/base.py

from sqlalchemy.orm import declarative_base

#データベースのテーブル構造をPythonのクラスで定義するための基底クラス
# modelsフォルダ内のすべてのクラスがこのBaseを継承する
# これにより、modelsフォルダ内のすべてのクラスがSQLAlchemyのモデルとして扱える
Base = declarative_base()