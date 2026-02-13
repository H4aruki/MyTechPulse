# app/models/tag.py

from sqlalchemy import Column, Integer, String
from sqlalchemy.orm import relationship
from .base import Base # 共通のBaseをインポート

class Tag(Base):
    __tablename__ = 'tag'

    tag_ID = Column(Integer, primary_key=True)
    tag_name = Column(String(50), nullable=False, unique=True)

    users = relationship("Recommend", back_populates="tag", cascade="all, delete-orphan")