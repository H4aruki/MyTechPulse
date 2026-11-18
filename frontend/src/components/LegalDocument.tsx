import type { ReactNode } from 'react'
import { LEGAL_VERSION } from '@/lib/legal'
import { AppLayout } from './AppLayout'

interface LegalDocumentProps {
  title: string
  children: ReactNode
}

/** 利用規約とプライバシーポリシーで共通の枠。見出し、版、施行日の欄を出す */
export function LegalDocument({ title, children }: LegalDocumentProps) {
  return (
    <AppLayout>
      <div className="mx-auto w-full max-w-3xl px-4 py-12">
        <h1 className="mb-2 text-2xl font-bold text-ink">{title}</h1>
        <p className="mb-8 text-sm text-ink-muted">
          版: {LEGAL_VERSION}（下書き） / 施行日: 【要記入：施行日】
        </p>
        <div className="space-y-8 text-sm leading-relaxed text-ink">{children}</div>
      </div>
    </AppLayout>
  )
}

interface LegalSectionProps {
  title: string
  children: ReactNode
}

export function LegalSection({ title, children }: LegalSectionProps) {
  return (
    <section className="space-y-3">
      <h2 className="text-lg font-bold text-ink">{title}</h2>
      {children}
    </section>
  )
}
