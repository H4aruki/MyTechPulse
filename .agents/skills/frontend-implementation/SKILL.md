---
name: frontend-implementation
description: Implements MyTechPulse React and TypeScript UI changes consistently. Use for components, pages, styles, routing, API calls, accessibility, responsive behavior, or browser interaction tests.
---

# 画面を実装する

1. 公開ページの `frontend/index.html` と、`/app/` 配下のReact SPAを混同しない。
2. 既存コンポーネント、色、余白、API層を再利用し、画面内で直接通信処理を重複させない。
3. API変更は `api-contract`、認証を含む変更は `auth-security-review` も使う。
4. 読み込み中、空、失敗、成功を表示し、キーボード操作、ラベル、フォーカス、色の見分けやすさを確認する。
5. デスクトップと狭い画面で崩れを確認する。
6. `npm run lint` と `npm run build` を実行する。操作変更は可能なら `webapp-testing` で実際のブラウザ挙動も確認する。
