/**
 * トップページと同じ、一行のフッター。
 */
export function SiteFooter() {
  return (
    <footer className="mt-auto border-t border-line bg-surface">
      <div className="mx-auto flex max-w-5xl flex-col items-center justify-center gap-2 px-6 py-10 sm:flex-row sm:gap-8">
        <p className="font-mono text-xs font-medium text-ink-60">Open Hack U 2025 KANAZAWA</p>
        <p className="text-xs text-ink-60">Created by Harry & K.Haruki</p>
      </div>
    </footer>
  )
}
