# app/models/recommend.py

from sqlalchemy import Column, Integer, ForeignKey
from sqlalchemy.orm import relationship
from .base import Base # 共通のBaseをインポート

class Recommend(Base):
    __tablename__ = 'recommend'

    user_ID = Column(Integer, ForeignKey('user.user_ID', ondelete='CASCADE'), primary_key=True)
    tag_ID = Column(Integer, ForeignKey('tag.tag_ID', ondelete='CASCADE'), primary_key=True)
    match_int = Column(Integer, nullable=False)

    user = relationship("User", back_populates="tags")
    tag = relationship("Tag", back_populates="users")