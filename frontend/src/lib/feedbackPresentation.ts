import type { FeedbackForm, FeedbackPresentationResponse } from '@/api/types'

/** 表示要求の結果のうち、質問を画面に出してよいもの */
export type ShownPresentation = FeedbackPresentationResponse & {
  prompt_id: string
  status: 'shown'
  form: FeedbackForm
}

export function isShownPresentation(p: FeedbackPresentationResponse): p is ShownPresentation {
  return p.eligible && p.status === 'shown' && !!p.prompt_id && !!p.form
}
