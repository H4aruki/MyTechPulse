# オリジンへの直接アクセスを塞ぐ

本番の API サーバー（AWS Lightsail。以降、オリジン）へ、Cloudflare を通さずに直接つながれないようにする手順です。本番の設定を変えるのはオーナーです。この資料は、その手順と、変えたあとに手元で確かめるコマンドだけを書きます。

関連する Issue は [#251](https://github.com/H4aruki/MyTechPulse/issues/251) と [#74](https://github.com/H4aruki/MyTechPulse/issues/74) です。#251 は、ファイアウォールの残りを #74、入口の拒否を #218 と整理しています。サーバーのアドレスは書きません。#74 の本文には作業当時のアドレスが残っていますが、ここへは写しません。確認結果を Issue に残すときも、アドレスは書かないでください。

作り直すときの全体手順は [lightsail-provisioning.md](lightsail-provisioning.md) です。通常の更新は [go-auto-deploy.md](go-auto-deploy.md) です。

## 作業中に守ること

> **22番（SSH）は、今の開き方のまま残す。** 80番や443番の変更に失敗しても、22番が開いていればサーバーへ入って直せます。AWS CLI の `put-instance-public-ports` は、リクエストに書いたポートだけを開き、書き忘れたポートを閉じます。22番を落とすと、SSH が届かなくなります。そのとき開けるのは SSH ではなく、Lightsail コンソールの Networking です。

あわせて守ること:

- アドレス（IPv4 も IPv6 も、古いものも新しいものも）を、リポジトリ、Issue、PR、チャット、この資料へ書かない。コマンドの出力にアドレスが含まれるものは、貼らずに消す。
- いま開いている SSH は、確認が終わるまで閉じない。鍵で入れるかは、別の端末で確かめる。
- API の DNS はプロキシ経由（オレンジの雲）のままにする。DNS only（グレーの雲）にすると、名前の問い合わせがオリジンのアドレスを返し、新しいアドレスを公開することになる。
- 80番を全世界へ開け戻す必要はない。証明書の更新（HTTP-01）は、プロキシ経由のとき Cloudflare からオリジンへ届く。範囲に Cloudflare が入っていれば更新は止まらない（[Caddyfile](../../Caddyfile)）。

## いまの守りと、この手順で足すもの

| 層 | いま | この手順 |
| --- | --- | --- |
| DNS | API の名前は Cloudflare のプロキシ経由。画面は Cloudflare Pages | 変えない。付け替えるときもプロキシはオンのまま |
| Caddy | Cloudflare の範囲以外には、本文なしの 403 を返す（`cloudflare_only`） | 変えない。範囲の正は、下のスクリプトと生成物 |
| Lightsail のファイアウォール | #251 の時点では、80/443 を範囲で絞る作業が残っている | 80/443 を、Caddy と同じ範囲だけにする。22番は残す |

Caddy の 403 は、接続を一度受けてから拒否します。拒否の処理はサーバーに届きます。ファイアウォールで落とせば、80/443 のパケットはサーバーに届きません。

始める前に、コンソールの Networking で今のルールを見ます。80/443 がすでに同じ範囲だけなら、同じ変更は重ねません。確認コマンド（5章）だけ実行します。

採らないもの:

- **文章からアドレスを消すだけ。** 履歴と、公開 DNS に載っていた間の外部の記録が残るため、#251 は効果を小さいとしていません。
- **Cloudflare Tunnel にして受け口を全部閉じる。** 常駐する分メモリを使い、1GB のプランでは重い、と #74 で判断済みです。
- **サーバーの iptables をこの手順の本体にする。** #74 では、Lightsail のルール数の上限を避ける案として挙がっていました。Docker が公開したポートは `INPUT` を通らないため、書くなら `DOCKER-USER` です。#251 が残作業にしているのは Lightsail のファイアウォールで、コンソールと CLI からサーバーへ入らずに変えられます。この資料はそちらだけを書きます。CLI でも件数の上限で入らないときは、22番を消さずに手を止め、iptables へは進みません。

## 作業の順番

1. SSH が鍵だけでしか入れないことを確かめる（1章）。
2. Cloudflare の範囲が、Caddy の生成物と一致することを確かめる（2章）。
3. ファイアウォールを変える（3章のコンソール、または 4章の CLI）。コンソールが件数で拒んだら、22番は触らず CLI へ移る。
4. 手元の PC から、直接は届かず、Cloudflare 経由では届くことを確かめる（5章）。
5. 固定 IP を付け替えるかは、そのあとオーナーが決める（6章）。
6. 結果を Issue #251 に残す（8章）。アドレスは書かない。

## 1. SSH が鍵だけでしか入れないことを確かめる

サーバーへ入り、次を実行します。`sudo` が要ることがあります。

```bash
sudo sshd -T | grep -E 'passwordauthentication|permitrootlogin|pubkeyauthentication|kbdinteractiveauthentication|challengeresponseauthentication'
```

`sshd -T` は、いま効いている値を小文字で出します。狙いと読み方は次のとおりです。

| 表示 | 狙い | 違うとき |
| --- | --- | --- |
| `passwordauthentication no` | パスワードでは入れない | `yes` なら、パスワードで入れる余地がある |
| `kbdinteractiveauthentication no` | 対話式のパスワードログインも無効 | `yes` なら、パスワードに近い入口が残っている |
| `challengeresponseauthentication no` | 上の古い名前。出たときだけ見る | `yes` なら、対話式が残っている |
| `permitrootlogin no` | root では入れない | `yes` は root で入れる。`prohibit-password` と `without-password` は、root のパスワードだけを止める値で、このサーバーの狙い（root では入れない）とは違う |
| `pubkeyauthentication yes` | 鍵では入れる | `no` なら鍵で入れない。ファイアウォールを変えない |

行が複数出るときは、`Match` で条件が分かれています。ログインに使う `ubuntu` に効く値を見ます。grep が何も出さないときは、コマンドが失敗しています。ファイアウォールへ進みません。

狙いと違うときは、セットアップスクリプト全体は本番でやり直しません（Docker や fail2ban も触るため）。SSH のドロップインだけを、[ops/lightsail-vm-setup.sh](../../ops/lightsail-vm-setup.sh) と同じ次の内容にします。

```
# MyTechPulse: 本番VMのSSH硬化設定（ops/lightsail-vm-setup.sh が生成）
PasswordAuthentication no
KbdInteractiveAuthentication no
PermitRootLogin no
```

置き場所は `/etc/ssh/sshd_config.d/99-mytechpulse-hardening.conf` です。反映の前に構文を確かめ、失敗したらファイルを戻します。いまの接続は閉じません。

```bash
sudo sshd -t && sudo systemctl try-reload-or-restart ssh
```

`sshd -t` が失敗したときは、reload しません。

ファイアウォールを変える前に、別の端末で次の2つを実行します。引用符の中の `<ORIGIN_IP>` だけを、手元の静的 IP に置き換えます。山かっこを引用符の外に置くと、シェルがリダイレクトとして読みます。アドレスは履歴に残るので、このコマンドの全文は共有しません。

```bash
ssh -i ~/.ssh/mytechpulse_lightsail "ubuntu@<ORIGIN_IP>"
```

鍵でシェルに入れること。

```bash
ssh -o PreferredAuthentications=password -o PubkeyAuthentication=no -o NumberOfPasswordPrompts=1 "ubuntu@<ORIGIN_IP>"
```

パスワードだけでは入れないこと（`Permission denied` で終わる）。自動デプロイの鍵は別の Secret（`LIGHTSAIL_SSH_KEY`）です。この2つのコマンドが確かめるのは、構築時の鍵と、パスワードが無効なことです。

## 2. Cloudflare の範囲を Caddy と揃える

範囲の取り方は [scripts/update-cloudflare-ips.mjs](../../scripts/update-cloudflare-ips.mjs) と同じです。取得元は `https://api.cloudflare.com/client/v4/ips` の `result.ipv4_cidrs` と `result.ipv6_cidrs` です。Caddy は、この一覧を [ops/caddy/cloudflare-ips.caddy](../../ops/caddy/cloudflare-ips.caddy) と [ops/caddy/cloudflare-trusted-proxies.caddy](../../ops/caddy/cloudflare-trusted-proxies.caddy) に書いて使います。ファイアウォールも、この2ファイルと同じ一覧にします。ブラウザの別一覧と数が違ったら、こちらの `--check` が成功する方へ合わせます。

リポジトリのルートで実行します。

```bash
node scripts/update-cloudflare-ips.mjs --check
echo "exit:$?"
```

終了コード 0 なら、日付の行を除いて生成物は API と一致しています。0 以外のときは、ファイアウォールだけを先に変えません。`node scripts/update-cloudflare-ips.mjs` の差分を PR にしてマージし、Caddy が本番へ反映されてから、同じ一覧でファイアウォールを変えます。先にファイアウォールだけを新しい範囲へ広げると、Caddy がまだ知らない範囲は 403 になります。先に Caddy だけを広げると、ファイアウォールが新しい範囲を落とします。

一覧の全文は、コンソールへ写すときだけ画面に出します。Issue には件数だけを書きます。

```bash
node --input-type=module - <<'EOF'
import { readFile } from "node:fs/promises";
import { fetchIps } from "./scripts/update-cloudflare-ips.mjs";
const cidrs = await fetchIps();
const v4 = cidrs.filter((c) => !c.includes(":"));
const v6 = cidrs.filter((c) => c.includes(":"));
const caddy = await readFile("ops/caddy/cloudflare-ips.caddy", "utf8");
const trusted = await readFile("ops/caddy/cloudflare-trusted-proxies.caddy", "utf8");
const missing = cidrs.filter((c) => !caddy.includes(c) || !trusted.includes(c));
console.log(`v4=${v4.length} v6=${v6.length} missing_in_caddy=${missing.length}`);
EOF
```

`missing_in_caddy=0` であること。この資料を用意した日（2026-11-13）は、`--check` が成功し、生成物は IPv4 が 15 件、IPv6 が 7 件でした。件数は変わるので、作業の日に数え直します。

コンソールへ写す一覧が要るときだけ、次で画面に出します。これは Cloudflare の公開範囲で、オリジンのアドレスではありません。出力は Issue に貼りません。

```bash
node --input-type=module - <<'EOF'
import { fetchIps } from "./scripts/update-cloudflare-ips.mjs";
const cidrs = await fetchIps();
console.log("# IPv4");
for (const c of cidrs.filter((c) => !c.includes(":"))) console.log(c);
console.log("# IPv6");
for (const c of cidrs.filter((c) => c.includes(":"))) console.log(c);
EOF
```

範囲が変わったあとの点検は、毎週の [cloudflare-ips.yml](../../.github/workflows/cloudflare-ips.yml) が失敗で知らせます。失敗したら生成物を更新する PR を作り、マージ後に Lightsail の 80/443 も同じ一覧へ直します。

## 3. コンソールで 80/443 を絞る

Lightsail コンソール → Instances → 対象のインスタンス → **Networking**。

IPv4 と IPv6 は別のファイアウォールです。IPv6 の欄が無いインスタンスは、IPv6 が無効です。無効なら IPv6 の手順は飛ばします。API の名前に AAAA レコードがあり、その向き先がこのサーバーなら、IPv6 経由の抜け道です。IPv6 を有効にして同じ範囲へ絞るか、AAAA を外すかをその場で決めます。アドレスは記録しません。

1. SSH（TCP 22）の行は、編集しない。消さない。「Allow Lightsail browser SSH」のチェックも変えない。
2. IPv4 の HTTP（TCP 80）を編集し、**Restrict to IP address** にする。送信元を、2章の IPv4 範囲だけにする。HTTPS（TCP 443）も同じ範囲にする。
3. 同じポートに「すべてのアドレス」のルールが残っていないことを見る。AWS の説明では、同じポートに複数のルールがあると、より広い方が使われます。すべてのアドレスが残っていると、範囲を足しても誰でも届きます。
4. IPv6 を有効にしているなら、IPv6 ファイアウォールの 80 と 443 も、2章の IPv6 範囲だけにする。22番は変えない。

AWS の案内（2026-11-13 に確認した [ファイアウォールの説明](https://docs.aws.amazon.com/lightsail/latest/userguide/understanding-firewall-and-port-mappings-in-amazon-lightsail.html)）では、コンソールは一度に送信元 30 件まで、API・CLI・SDK は IPv4 と IPv6 でそれぞれ 60 件までです。ポートごとに送信元を 1 件と数えると、IPv4 は「範囲の件数 × 2（80 と 443）」です。15 件なら 30 件で、コンソールの上限と同じ数になり、SSH の分を足すと超えます。コンソールが保存を拒んだら、保存されていない状態で止めます。22番を消して枠を空けません。4章の CLI へ移ります。

証明書のために 80番を全世界へ開いたままにはしません。

## 4. AWS CLI で 80/443 を絞る

AWS CLI v2 を使います。リージョンは東京（`ap-northeast-1`）です。`<INSTANCE_NAME>` は、実行前にインスタンス名へ置き換え、引用符は残します。

`put-instance-public-ports` は、指定したポート以外を閉じます。次の手順は、今の 22番をそのまま写し、80/443 だけを差し替えます。22番を新しく「全世界へ開く」ルールへ作り直すことはしません。

### 4-1. 今のルールを、リポジトリの外へ保存する

```bash
aws lightsail get-instance-port-states \
  --region ap-northeast-1 \
  --instance-name "<INSTANCE_NAME>" \
  --output json > /tmp/lightsail-ports-before.json
```

このファイルには送信元の範囲が入ります。リポジトリへ移動しない。Issue に貼らない。

### 4-2. 置き換え用の JSON を作る

リポジトリのルートで実行します。出力先は `/tmp` だけです。スクリプトは、22番が 1 件で無いとき、出力先がリポジトリの中のとき、インスタンス名に山かっこが残っているとき、80/443 が全世界のままのとき、送信元が 60 件を超えるときは、ファイルを書かずに終了コード 2 で止まります。

```bash
node --input-type=module - /tmp/lightsail-ports-before.json /tmp/lightsail-put.json "<INSTANCE_NAME>" <<'END_SCRIPT'
import { relative, resolve, isAbsolute } from "node:path";
import { readFile, writeFile } from "node:fs/promises";
import { fetchIps } from "./scripts/update-cloudflare-ips.mjs";

const [beforePath, afterPath, instanceName] = process.argv.slice(2);

function fail(message) {
  console.error(message);
  process.exit(2);
}

function isInsideRepo(p) {
  const rel = relative(process.cwd(), resolve(p));
  return rel === "" || (!rel.startsWith("..") && !isAbsolute(rel));
}

if (!beforePath || !afterPath || !instanceName) {
  fail("引数が足りません。before.json after.json インスタンス名 の3つを渡してください。");
}
if (isInsideRepo(beforePath) || isInsideRepo(afterPath)) {
  fail("入出力ファイルはリポジトリの外（例: /tmp）にしてください。ファイルは書きません。");
}
if (!/^[\w][\w-]*[\w]$/.test(instanceName)) {
  fail("インスタンス名の形が違います。山かっこを含んだままでは実行しません。ファイルは書きません。");
}

let before;
try {
  before = JSON.parse(await readFile(beforePath, "utf8"));
} catch {
  fail("取得済み JSON を読めません。get-instance-port-states の --output json を指定してください。");
}
const states = before.portStates;
if (!Array.isArray(states) || states.length === 0) {
  fail("portStates がありません。ファイルは書きません。");
}

function protocolOf(p) {
  return String(p.protocol || "").toLowerCase();
}

function coversWeb(p) {
  if (protocolOf(p) !== "tcp") return false;
  const from = Number(p.fromPort);
  const to = Number(p.toPort);
  const hit = (port) => from <= port && port <= to;
  return hit(80) || hit(443);
}

const keep = states.filter((p) => !coversWeb(p));
const ssh = keep.filter((p) => protocolOf(p) === "tcp" && Number(p.fromPort) === 22 && Number(p.toPort) === 22);
if (ssh.length !== 1) {
  fail("22番の TCP ルールが1件ではありません。80や443を含む広いルールしか無い可能性もあります。ファイルは書きません。");
}

const cidrs = await fetchIps();
const v4 = cidrs.filter((c) => !c.includes(":"));
const v6 = cidrs.filter((c) => c.includes(":"));
if (v4.length === 0 || v6.length === 0) {
  fail("Cloudflare の範囲が空です。ファイルは書きません。");
}
const ipv6Enabled = states.some((p) => Array.isArray(p.ipv6Cidrs) && p.ipv6Cidrs.length > 0);

function strip(p) {
  const out = {
    fromPort: Number(p.fromPort),
    toPort: Number(p.toPort),
    protocol: protocolOf(p),
  };
  if (Array.isArray(p.cidrs) && p.cidrs.length > 0) out.cidrs = p.cidrs;
  if (Array.isArray(p.ipv6Cidrs) && p.ipv6Cidrs.length > 0) out.ipv6Cidrs = p.ipv6Cidrs;
  if (Array.isArray(p.cidrListAliases) && p.cidrListAliases.length > 0) out.cidrListAliases = p.cidrListAliases;
  return out;
}

function web(port) {
  const item = { fromPort: port, toPort: port, protocol: "tcp", cidrs: v4 };
  if (ipv6Enabled) item.ipv6Cidrs = v6;
  return item;
}

const portInfos = [...keep.map(strip), web(80), web(443)];
if (!portInfos.some((p) => p.protocol === "tcp" && p.fromPort === 22 && p.toPort === 22)) {
  fail("22番が結果から消えました。ファイルは書きません。");
}
for (const p of portInfos) {
  if ((p.fromPort === 80 || p.fromPort === 443) && p.toPort === p.fromPort && p.protocol === "tcp") {
    if ((p.cidrs || []).some((c) => c === "0.0.0.0/0") || (p.ipv6Cidrs || []).some((c) => c === "::/0")) {
      fail("80/443 が全世界へ開いたままです。ファイルは書きません。");
    }
  }
}

const v4Sources = portInfos.reduce((n, p) => n + (p.cidrs || []).length, 0);
const v6Sources = portInfos.reduce((n, p) => n + (p.ipv6Cidrs || []).length, 0);
if (v4Sources > 60 || v6Sources > 60) {
  fail(`送信元の件数が案内の上限（各60）を超えています。v4=${v4Sources} v6=${v6Sources}。ファイルは書きません。`);
}

await writeFile(afterPath, JSON.stringify({ instanceName, portInfos }, null, 2) + "\n");
console.log(`wrote ${afterPath}`);
console.log(`kept_rules=${keep.length} ssh_rules=${ssh.length} v4=${v4.length} v6=${v6.length} ipv6_enabled=${ipv6Enabled} ipv4_sources=${v4Sources} ipv6_sources=${v6Sources}`);
console.log("22番は残しています。80/443 だけを Cloudflare の範囲に差し替えました。このファイルはリポジトリに置かず、作業後に消してください。");
END_SCRIPT
```

成功すると、件数だけが出ます。`ssh_rules=1` であること。`v4` と `v6` は、2章で数えた件数と一致すること。

送る前に、アドレスを出さずに形だけ見ます。

```bash
python3 - <<'PY'
import json
data = json.load(open("/tmp/lightsail-put.json"))
infos = data["portInfos"]
print("instance_name_set", bool(data.get("instanceName")))
for p in infos:
    web = p.get("protocol") == "tcp" and p.get("fromPort") in (80, 443) and p.get("toPort") == p.get("fromPort")
    world = any(c == "0.0.0.0/0" for c in (p.get("cidrs") or []))
    world6 = any(c == "::/0" for c in (p.get("ipv6Cidrs") or []))
    print(
        f"port={p.get('fromPort')} proto={p.get('protocol')} "
        f"v4={len(p.get('cidrs') or [])} v6={len(p.get('ipv6Cidrs') or [])} "
        f"web_open_to_world={(world or world6) if web else 'n/a'}"
    )
ssh = [p for p in infos if p.get("protocol") == "tcp" and p.get("fromPort") == 22 and p.get("toPort") == 22]
print("ssh_rules", len(ssh))
PY
```

`ssh_rules` が 1、80 と 443 の `web_open_to_world` が `False` であること。違うときは `put-instance-public-ports` を実行しません。

キー名は CLI の版で変わることがあります。送る前に、ローカルで次を実行し、`instanceName` と `portInfos` があることを見ます。このコマンドは AWS へ通信しません。

```bash
aws lightsail put-instance-public-ports --generate-cli-skeleton
```

### 4-3. 適用する

22番が結果に残っていることを、4-2 の表示で見てから実行します。

```bash
aws lightsail put-instance-public-ports \
  --region ap-northeast-1 \
  --cli-input-json file:///tmp/lightsail-put.json
```

戻り値の操作が失敗していたら、5章へ進みません。コンソールで 22番がまだ開いているかを見ます。閉じているときは、コンソールから TCP 22 を開け直します。

適用後、件数だけを見ます。生の JSON は画面に出しません。

```bash
aws lightsail get-instance-port-states \
  --region ap-northeast-1 \
  --instance-name "<INSTANCE_NAME>" \
  --output json > /tmp/lightsail-ports-after.json
python3 - <<'PY'
import json
d = json.load(open("/tmp/lightsail-ports-after.json"))
for p in d.get("portStates", []):
    web = p.get("protocol") == "tcp" and p.get("fromPort") in (80, 443) and p.get("toPort") == p.get("fromPort")
    world = any(c == "0.0.0.0/0" for c in (p.get("cidrs") or []))
    world6 = any(c == "::/0" for c in (p.get("ipv6Cidrs") or []))
    print(
        f"port={p.get('fromPort')} proto={p.get('protocol')} state={p.get('state')} "
        f"v4={len(p.get('cidrs') or [])} v6={len(p.get('ipv6Cidrs') or [])} "
        f"web_open_to_world={(world or world6) if web else 'n/a'}"
    )
PY
```

22番が `open`、80 と 443 の `web_open_to_world` が `False` であること。80番（と 443番）の `v4` が 2章の IPv4 の件数と一致すること。行が分かれているときは、そのポートの `v4` を足した数が一致すればよいです。

別の端末で、鍵の SSH がまだ入れることを確かめます。入れないときは、コンソールで 22番を開け、保存した `/tmp/lightsail-ports-before.json` をそのまま `--cli-input-json` に渡して戻そうとしないでください（`portStates` と `state` があり、送る形と違います）。80/443 の戻し方は、末尾の「戻し方」です。

確認が終わったら、適用用と確認用の JSON を消します。戻す可能性が残っている間は、適用前の `/tmp/lightsail-ports-before.json` だけ残してよいです。戻す作業が終わったら、それも消します。どれもリポジトリには置きません。

```bash
rm -f /tmp/lightsail-put.json /tmp/lightsail-ports-after.json
```

## 5. 外から確かめる

手元の PC から実行します。サーバーの中から自分自身へつなぐと、ファイアウォールを通らないことがあります。

`--resolve` は、名前の問い合わせを使わず、指定したアドレスへ直接つなぎます。これを付けない `curl https://api.mytechpulse.net/...` は Cloudflare 経由です。直接の確認では、必ず `--resolve` を付けます。

`<ORIGIN_IP>` は引用符の中だけを、手元の静的 IP に置き換えます。API の名前が `api.mytechpulse.net` と違うときは、本番の設定 `API_DOMAIN` の値に読み替えます。出力にアドレスが出ても、Issue には「タイムアウトした」「200 だった」だけを書きます。

直接（443）。狙いはタイムアウトです。

```bash
curl -sS -o /dev/null -w 'http_code=%{http_code}\n' --max-time 15 \
  --resolve "api.mytechpulse.net:443:<ORIGIN_IP>" \
  https://api.mytechpulse.net/health/live || echo "exit:$?"
```

直接（80）。

```bash
curl -sS -o /dev/null -w 'http_code=%{http_code}\n' --max-time 15 \
  --resolve "api.mytechpulse.net:80:<ORIGIN_IP>" \
  http://api.mytechpulse.net/health/live || echo "exit:$?"
```

| 結果 | 意味 |
| --- | --- |
| `exit:28` かつ `http_code=000` | 時間内に応答が無い。ファイアウォールが捨てている。狙いどおり |
| すぐに `http_code=403` | サーバーまで届いている。Caddy が拒否しているだけで、ファイアウォールはまだ開いている |
| `http_code=200` | オリジンが直接応答している。ここで止め、ルールを見直す |
| `exit:7`（connection refused） | ポートまで届いている。ファイアウォールは開いている |

ポートだけの確認が要るときは、次もタイムアウトになることを見ます。`curl` の方が、本番の名前を付けた接続まで見られます。

```bash
nc -vz -w 5 "<ORIGIN_IP>" 443
```

Cloudflare 経由。`--resolve` は付けません。`/health/live` はプロセスの生存、`/health/ready` は DB までです。自動デプロイが見るのは `/health/ready` です。

```bash
curl -sS -o /dev/null -w 'live=%{http_code}\n' --max-time 15 \
  https://api.mytechpulse.net/health/live
curl -sS -o /dev/null -w 'ready=%{http_code}\n' --max-time 15 \
  https://api.mytechpulse.net/health/ready
```

両方 200 であること。live が 200 で ready が 503 のときは、入口は通っていて DB が応答していません。ファイアウォールを全世界へ戻す理由にはなりません。

IPv6 を有効にしているときだけ、直接の IPv6 もタイムアウトになることを見ます。引用符の中の `<ORIGIN_IPV6>` だけをアドレスに置き換え、アドレスを囲む `[` `]` は残します。

```bash
curl -sS -g -o /dev/null -w 'http_code=%{http_code}\n' --max-time 15 \
  --resolve "api.mytechpulse.net:443:[<ORIGIN_IPV6>]" \
  https://api.mytechpulse.net/health/live || echo "exit:$?"
```

## 6. 固定 IP を付け替えるか

ファイアウォールのあとに、オーナーが決めます。付け替えは、Lightsail の静的 IP を新しいものへ交換し、DNS と自動デプロイの接続先を同じ作業の中で直すことです。インスタンスの作り直しではありません。

ファイアウォールはインスタンスに付きます。アドレスを交換しても、80/443 の範囲制限はそのまま残ります。

| | 付け替える | 付け替えない |
| --- | --- | --- |
| 届かなくなるもの | 履歴や外部の記録に残った古いアドレスでは、このサーバーへ届かない。22番も、古いアドレスでは届かない | 80/443 の直接到達は、ファイアウォールだけで止まる |
| 残るもの | 新しいアドレスで 22番は開いたまま（この作業では閉じない） | 公開されたアドレスを知っていれば、22番の入口はそのアドレスのまま残る。パスワードログインは 1章で無効であることを確認済み、が前提 |
| 作業と失敗 | DNS の A レコードと、Secret `LIGHTSAIL_HOST` を同じ作業で直す。プロキシを外すと、新しいアドレスを公開する。自動デプロイを止めずに交換すると、古い接続先へ向けて失敗する | ファイアウォールと 5章の確認だけで終わる |
| 費用 | インスタンスに付いていない静的 IP は課金される（[lightsail-provisioning.md](lightsail-provisioning.md)）。確認が終わったら古い方を解放し、新しい方を付けたままにする | 追加の静的 IP は作らない |

付け替える価値は、80/443 を塞いだあとも残る「すでに公開されたアドレス」（とくに 22番）を、このサーバーのアドレスではなくすことです。ファイアウォールだけでは、履歴に残ったアドレスはこのサーバーを指したままです。

同じ日にファイアウォールから続けてよいです。DNS と Secret を触る時間が取れない日には、付け替えません。

### 付け替えると決めたとき

画面用のレコード（apex や www。Cloudflare Pages 向け）は変えません。変えるのは、API の名前（`API_DOMAIN`。例は `api.mytechpulse.net`）の A レコードだけです。

1. 自動デプロイを止める。リポジトリ変数 `GO_DEPLOY_ENABLED` を `false` にするか、変数を外します。止め方は [go-auto-deploy.md](go-auto-deploy.md) の「止めたいとき」です。
2. 新しい静的 IP を作る。コンソールなら Networking → **Create static IP**。CLI なら次です。名前は手元で決め、資料には書きません。

```bash
aws lightsail allocate-static-ip \
  --region ap-northeast-1 \
  --static-ip-name "<NEW_STATIC_IP_NAME>"
aws lightsail get-static-ip \
  --region ap-northeast-1 \
  --static-ip-name "<NEW_STATIC_IP_NAME>"
```

`get-static-ip` の出力に、新しいアドレスそのものが出ます。画面で見て手元だけに控え、出力は貼りません。

3. Cloudflare の DNS で、API の A レコードの編集画面を開いておく。プロキシはオンのまま。まだ保存しない。
4. 古い静的 IP を外し、新しい静的 IP を同じインスタンスへ付ける。外した瞬間、公開アドレスは変わります。間隔を空けません。1台に静的 IP は1つです。

```bash
aws lightsail detach-static-ip \
  --region ap-northeast-1 \
  --static-ip-name "<OLD_STATIC_IP_NAME>"
aws lightsail attach-static-ip \
  --region ap-northeast-1 \
  --static-ip-name "<NEW_STATIC_IP_NAME>" \
  --instance-name "<INSTANCE_NAME>"
```

5. API の A レコードの中身を、新しいアドレスへ保存する。プロキシはオンのまま（オレンジの雲）。
6. 名前が、新しいアドレスそのものを返していないことを、手元で比べる。出力は貼りません。

```bash
dig +short api.mytechpulse.net A
```

応答のどれかが、手元に控えた新しいアドレスと一致したら、プロキシが外れています。すぐにプロキシをオンへ戻します。一致しなければ、問い合わせは Cloudflare のアドレスを返しています。

7. GitHub の Settings → Secrets and variables → Actions → Secrets で、`LIGHTSAIL_HOST` の値を新しいアドレスへ更新する。`LIGHTSAIL_SSH_KEY` と `LIGHTSAIL_USER` は、同じインスタンスなら変えません。値は書かない。ワークフローは、この Secret と、すでに [ci.yml](../../.github/workflows/ci.yml) に書いてあるサーバーのホスト鍵から known_hosts を作ります。インスタンスが同じならホスト鍵は変わりません。アドレスをワークフローへ書き足しません。名前の説明は [ci-and-release.md](ci-and-release.md) の「必要な秘密」です。
8. 5章を、新しいアドレスでやり直す。直接はタイムアウト、Cloudflare 経由は 200。別の端末から鍵で SSH できること。
9. `GO_DEPLOY_ENABLED` を、止める前の状態へ戻す。
10. ここまで成功してから、古い静的 IP を解放する。失敗している間は解放しません。外したままの静的 IP は課金されるので、作業を日をまたいで放置しません。

```bash
aws lightsail release-static-ip \
  --region ap-northeast-1 \
  --static-ip-name "<OLD_STATIC_IP_NAME>"
```

解放したアドレスは AWS のプールへ戻ります。戻したあとは、このサーバーのアドレスではありません。

## 7. 新しいアドレスをリポジトリへ書かせない仕組み（gitleaks）

結論: **いまの設定では、IPv4 アドレスを検出できません。** この作業では [.gitleaks.toml](../../.gitleaks.toml) を変えません。足すなら、下の形です。足す前に、試験コードへ残っている公開アドレスの整理が要ります。

調べたもの（2026-11-13、gitleaks 8.30.1。CI の `secret-scan` と同じ版です）:

- `.gitleaks.toml` は `useDefault = true` で、リポジトリ側が足しているのは `generic-api-key` と `aws-access-token` の許可リストだけです。新しい検出ルールはありません。
- 既定のルール一覧（v8.30.1 の `config/gitleaks.toml`）に、IPv4 アドレスそのものを検出する id はありません。
- 文書用の IPv4 を1つ書いたファイルを、このリポジトリの設定で検査すると、検出は 0 件でした。
- Issue #251 が本番のアドレスの記載先として挙げている `.claude/hooks/lib/rules.test.mjs` を、同じ設定で検査しても、検出は 0 件でした。アドレスは表示も転記もしていません。

そのため、コミット前の [.githooks/pre-commit](../../.githooks/pre-commit) も、CI の `secret-scan` も、アドレスが書かれても気づきません。`secret-scan` は必須の検査ではなく、失敗しても本番への反映は止まりません（[ci-and-release.md](ci-and-release.md)）。

足すときの形は、次のルールです。まだファイルへは入れません。キーワードを `.` にすると、点を含む行（IPv4 の行は必ず点を含む）を見ます。`host` のような語をキーワードにすると、アドレスだけの行を見逃します。gitleaks 8.30.1 で、この形は許可リストの外のアドレスを検出し、ループバックは許可リストで黙ることを、一時的な設定で確認しました（リポジトリの設定ファイルは変えていません）。

```toml
[[rules]]
id = "ipv4-address"
description = "リポジトリに書いた IPv4 アドレス"
regex = '''\b(?:(?:25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\.){3}(?:25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\b'''
keywords = ["."]

[[rules.allowlists]]
description = "Cloudflare の公開範囲（生成物）。オリジンのアドレスではない"
paths = [
  '''(^|/)ops/caddy/cloudflare-ips\.caddy$''',
  '''(^|/)ops/caddy/cloudflare-trusted-proxies\.caddy$''',
]

[[rules.allowlists]]
description = "ループバック、未指定、リンクローカル、私設網、文書用の例示範囲。本番のアドレスは許可しない"
regexTarget = "match"
regexes = [
  '''\b(?:0\.0\.0\.0|127(?:\.\d{1,3}){3}|169\.254(?:\.\d{1,3}){2}|10(?:\.\d{1,3}){3}|192\.168(?:\.\d{1,3}){2}|172\.(?:1[6-9]|2\d|3[0-1])(?:\.\d{1,3}){2}|192\.0\.2\.\d{1,3}|198\.51\.100\.\d{1,3}|203\.0\.113\.\d{1,3})\b''',
]
```

この一時的な設定で、追跡されているファイルだけを検査すると、生成物の2ファイルは許可リストで外れ、それでも **57 件、5 ファイル**が残りました。内訳は件数だけです。アドレスは出していません。

| ファイル | 件数 | 中身の分類（件数） |
| --- | --- | --- |
| `server/internal/auth/failure_paths_test.go` | 4 | よく知られた公開 DNS のアドレス 4 |
| `server/internal/auth/passkey_support_test.go` | 26 | 公開 DNS 25、それ以外の公開アドレス 1 |
| `server/internal/auth/policy_limiter_test.go` | 21 | 公開 DNS 17、それ以外の公開アドレス 4 |
| `scripts/update-cloudflare-ips.test.mjs` | 5 | Cloudflare の範囲に入るもの 4、それ以外の公開アドレス 1 |
| `.claude/hooks/lib/rules.test.mjs` | 1 | 公開 DNS でも Cloudflare の範囲でもない公開アドレス 1 |

最後のファイルは、#251 が本番のアドレスの記載先として挙げているファイルです。検査が見たのは「IPv4 が 1 件ある」ことまでです。このファイルを許可リストに入れると、検出したいものを見逃します。

ルールを今足すと、上の 57 件で検査が失敗します。足す前に、試験の例を文書用の範囲（`192.0.2.0/24`、`198.51.100.0/24`、`203.0.113.0/24`）へ置き換え、#251 が指すファイルから本番のアドレスを消します。本番のアドレス自体を許可リストへ書くことはしません。

この仕組みでできないこと:

- GitHub の Issue や PR の本文は見ません。
- すでに履歴へ入ったアドレスは、履歴を書き換えなければ残ります。PR の検査は、その PR の差分だけを見ます。
- IPv6 は、このルールでは見ません。
- CI の `secret-scan` は、失敗しても自動デプロイを止めません。手元の pre-commit は、gitleaks が入っているときだけ、ステージした変更を止めます。

## 8. Issue に残す記録

Issue #251 に、次をコピーして埋めます。アドレス、ルールの全文、`dig` や `get-static-ip` の出力は書かないでください。

```markdown
## 実施記録（オリジンへの直接アクセスを塞ぐ）

- 実施日:
- 実施した人:

### SSH
- `sudo sshd -T` の passwordauthentication:
- kbdinteractiveauthentication（出なければ「行なし」）:
- permitrootlogin:
- pubkeyauthentication:
- 別の端末で、鍵では入れた:
- 別の端末で、パスワードだけでは入れなかった:

### ファイアウォール
- `node scripts/update-cloudflare-ips.mjs --check` の終了コード:
- IPv4 の件数 / IPv6 の件数（アドレスは書かない）:
- 変えた場所: コンソール / AWS CLI
- 80/443 をその範囲だけにした:
- 22番は変更していない:
- 直接（curl --resolve）の 443 と 80: タイムアウト（exit 28）/ それ以外（コードだけ。アドレスは書かない）
- Cloudflare 経由の /health/live と /health/ready:

### 固定 IP
- 判断: 付け替えない / 付け替えた
- 理由:
- 付け替えた場合: API の A レコードはプロキシのままか、dig の応答は手元の静的 IP と一致しなかったか、Secret `LIGHTSAIL_HOST` を更新したか（値は書かない）、古い静的 IP を解放したか

### gitleaks
- ルールはこの作業では追加していない

### 残した問題
```

## 戻し方

利用者の経路（Cloudflare 経由）が失敗し、原因が範囲の入れ間違いであるときだけ、80/443 を元の「すべてのアドレス」へ戻します。22番は残します。Caddy の 403 は、ファイアウォールを戻しても残ります。Caddy の生成物も戻すときは、[lightsail-provisioning.md](lightsail-provisioning.md) のファイアウォールの節に書いた、生成物の revert とセットにします。

コンソールで戻すのが確実です。IPv4 と、有効なら IPv6 の、HTTP と HTTPS を、すべてのアドレスに戻します。22番が閉じているときだけ、TCP 22 を開け直します。開いているときは、SSH の行は変更しません。

CLI で戻すときも、`put-instance-public-ports` は書かなかったポートを閉じます。80 と 443 だけを送ると、22番が閉じます。戻すリクエストにも、今の 22番を含めます。適用前に保存した JSON は、キーが `portStates` で `state` を含むため、そのまま `--cli-input-json` に渡しません。
