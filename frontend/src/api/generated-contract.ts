import type { paths } from './generated'

type Login = paths['/api/v1/auth/login']['post']
type Feed = paths['/api/v1/feed']['get']
type Click = paths['/api/v1/feedback/article-clicks']['post']

export const generatedContractExists: [Login, Feed, Click] | null = null
