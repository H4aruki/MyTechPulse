import { LegalDocument, LegalSection } from '@/components/LegalDocument'

export function TermsPage() {
  return (
    <LegalDocument title="利用規約">
      <p>
        この利用規約（以下「本規約」）は、MyTechPulse（以下「本サービス」）の利用条件を定めます。本サービスの運営者は【要記入：運営者名】（【要記入：事業形態（個人／法人）】。以下「運営者」）です。
      </p>

      <LegalSection title="第1条（適用）">
        <p>
          本規約は、本サービスを利用するすべての方（以下「利用者」）に適用します。個人情報などの扱いは、別に定めるプライバシーポリシーによります。
        </p>
      </LegalSection>

      <LegalSection title="第2条（登録）">
        <ul className="list-disc space-y-1 pl-5">
          <li>利用者は、本規約とプライバシーポリシーに同意したうえで、会員登録をします。</li>
          <li>登録では、事実と異なる内容を入力してはいけません。</li>
          <li>利用者は、自分のユーザー名、パスワード、パスキーを自分で管理します。第三者に使わせたり、貸したりしてはいけません。</li>
          <li>未成年の方の利用条件は、【要記入：未成年の利用条件】とします。</li>
        </ul>
      </LegalSection>

      <LegalSection title="第3条（禁止事項）">
        <p>利用者は、次の行為をしてはいけません。</p>
        <ul className="list-disc space-y-1 pl-5">
          <li>法令または公序良俗に反する行為</li>
          <li>他人のアカウントを不正に使う行為</li>
          <li>本サービスや、Qiita・Zennなどの提供元のサーバーに過度な負荷をかける行為</li>
          <li>本サービスの運営を妨げる行為、不正アクセス、脆弱性の探索や悪用</li>
          <li>自動化した手段で、大量に登録やアクセスをする行為</li>
          <li>その他、運営者が不適切と判断する行為</li>
        </ul>
      </LegalSection>

      <LegalSection title="第4条（提供元の記事について）">
        <p>
          本サービスは、QiitaとZennで公開されている記事の題名や概要などへのリンクを表示します。記事の内容や権利は、それぞれの著者と提供元に属します。記事の正確さや、提供元の都合による変更・削除について、運営者は責任を負いません。
        </p>
      </LegalSection>

      <LegalSection title="第5条（サービスの変更・停止）">
        <p>
          運営者は、保守、障害、提供元の仕様変更などの事情があるときは、事前の通知なく、本サービスの内容を変更し、または一時的もしくは全部を停止することがあります。
        </p>
      </LegalSection>

      <LegalSection title="第6条（利用の停止）">
        <p>
          利用者が本規約に違反したときは、運営者は、事前の通知なく、その利用者の利用を止め、または登録を削除することがあります。
        </p>
      </LegalSection>

      <LegalSection title="第7条（免責）">
        <ul className="list-disc space-y-1 pl-5">
          <li>本サービスは、現状のまま提供します。おすすめの記事の順位や内容が、利用者の希望に合うことは保証しません。</li>
          <li>運営者は、本サービスの利用または利用できないことで利用者に生じた損害について、運営者に故意または重大な過失がある場合を除き、責任を負いません。</li>
          <li>【要記入：責任の上限など、免責に関する法的な定め】</li>
        </ul>
      </LegalSection>

      <LegalSection title="第8条（規約の変更）">
        <p>
          運営者は、必要と判断したときは、本規約を変更できます。変更後の規約は、本サービスの上に表示した時点から効力を持ちます。重要な変更は、【要記入：変更の通知方法】で知らせます。
        </p>
      </LegalSection>

      <LegalSection title="第9条（準拠法・管轄）">
        <p>
          本規約は、日本法に従って解釈します。本サービスに関して争いが生じたときは、【要記入：準拠法・管轄裁判所】を第一審の専属的合意管轄とします。
        </p>
      </LegalSection>

      <LegalSection title="運営者・お問い合わせ">
        <ul className="list-disc space-y-1 pl-5">
          <li>運営者名: 【要記入：運営者名】</li>
          <li>所在地: 【要記入：運営者の住所】</li>
          <li>お問い合わせ先: 【要記入：問い合わせ先メールアドレス】</li>
        </ul>
        <p>最終改定日: 【要記入：最終改定日】</p>
      </LegalSection>
    </LegalDocument>
  )
}
