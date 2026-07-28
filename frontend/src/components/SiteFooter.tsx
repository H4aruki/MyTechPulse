/**
 * 旧実装で4ファイルに重複していたフッターを共通化したもの。内容は既存を踏襲。
 */
export function SiteFooter() {
  return (
    <footer className="mt-auto bg-night text-slate-300">
      <div className="mx-auto grid max-w-6xl gap-8 px-4 py-10 sm:grid-cols-3">
        <div>
          <h3 className="mb-3 font-display text-sm font-bold tracking-wide text-white">
            Created by
          </h3>
          <p className="text-sm">Harry</p>
          <p className="text-sm">K.Haruki</p>
        </div>
        <div>
          <h3 className="mb-3 font-display text-sm font-bold tracking-wide text-white">Event</h3>
          <p className="text-sm">Open Hack U 2025 Kanazawa</p>
        </div>
        <div>
          <h3 className="mb-3 font-display text-sm font-bold tracking-wide text-white">
            Powered by
          </h3>
          <ul className="space-y-1 text-sm">
            <li>
              <a
                href="https://qiita.com/"
                target="_blank"
                rel="noreferrer noopener"
                className="transition hover:text-white"
              >
                Qiita
              </a>
            </li>
            <li>
              <a
                href="https://zenn.dev/"
                target="_blank"
                rel="noreferrer noopener"
                className="transition hover:text-white"
              >
                Zenn
              </a>
            </li>
          </ul>
        </div>
      </div>
    </footer>
  )
}
