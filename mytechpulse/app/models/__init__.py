# app/models/__init__.py
#このファイルのおかげでfrom .. import modelsという記述でmodelsフォルダ内のすべてのクラスをインポートできる

from .base import Base
from .user import User
from .tag import Tag
from .recommend import Recommend