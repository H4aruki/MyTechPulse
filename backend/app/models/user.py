# app/models/user.py

from sqlalchemy import Column, Integer, String
from sqlalchemy.orm import relationship
from .base import Base   # 共通のBaseをインポート

class User(Base):   #Baseを継承
    __tablename__ = 'user'   #このクラスがMySQlのテーブル名に対応することを示す

    user_ID = Column(Integer, primary_key=True)   # autoincrementはデフォルトで有効
    user_name = Column(String(50), nullable=False, unique=True)
    password = Column(String(255), nullable=False)

   #他のテーブルとの関連を定義
    tags = relationship("Recommend", back_populates="user", cascade="all, delete-orphan")