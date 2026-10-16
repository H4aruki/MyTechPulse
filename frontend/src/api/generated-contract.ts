import type { paths } from './generated'

type Login = paths['/api/v1/auth/login']['post']
type Feed = paths['/api/v1/feed']['get']
type Click = paths['/api/v1/feedback/article-clicks']['post']
type FeedbackStatus = paths['/api/v1/user-feedback/status']['get']
type FeedbackPresent = paths['/api/v1/user-feedback/presentations']['post']
type FeedbackSubmit = paths['/api/v1/user-feedback/submissions']['post']
type FeedbackComplete = paths['/api/v1/user-feedback/submissions/{submission_id}']['put']
type FeedbackDismiss = paths['/api/v1/user-feedback/dismissals']['post']
type PasskeyBegin = paths['/api/v1/auth/passkeys/register/begin']['post']
type PasskeyFinish = paths['/api/v1/auth/passkeys/register/finish']['post']
type PasskeyList = paths['/api/v1/auth/passkeys']['get']
type PasskeyRename = paths['/api/v1/auth/passkeys/{id}']['patch']
type PasskeyDelete = paths['/api/v1/auth/passkeys/{id}']['delete']

export const generatedContractExists:
  | [Login, Feed, Click, FeedbackStatus, FeedbackPresent, FeedbackSubmit, FeedbackComplete, FeedbackDismiss, PasskeyBegin, PasskeyFinish, PasskeyList, PasskeyRename, PasskeyDelete]
  | null = null
