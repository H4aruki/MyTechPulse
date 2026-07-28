import type { ReactNode } from 'react'
import { SiteFooter } from './SiteFooter'
import { SiteHeader } from './SiteHeader'

interface AppLayoutProps {
  children: ReactNode
  authenticated?: boolean
  onLogout?: () => void
}

export function AppLayout({ children, authenticated, onLogout }: AppLayoutProps) {
  return (
    <div className="flex min-h-dvh flex-col">
      <SiteHeader authenticated={authenticated} onLogout={onLogout} />
      <main className="flex-1">{children}</main>
      <SiteFooter />
    </div>
  )
}
