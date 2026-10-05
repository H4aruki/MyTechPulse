// Go版APIの仕様書（server/openapi/openapi.json）から自動生成した generated.ts への読みやすい別名。
// 型を手で書き写さない。仕様が変わったら `npm run api:generate` で作り直す。
import type { components } from './generated'

type Schemas = components['schemas']

export type User = Schemas['User']
export type Article = Schemas['FeedArticle']
export type FeedResponse = Schemas['FeedResponse']
export type FeedWarning = Schemas['Warning']
export type LoginRequest = Schemas['LoginRequest']
export type SignupRequest = Schemas['SignupRequest']
export type AuthResponse = Schemas['AuthResponse']
