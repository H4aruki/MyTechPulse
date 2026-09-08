import { test } from 'node:test';
import assert from 'node:assert/strict';
import { splitSegments, checkCommand } from './rules.mjs';

test('つなぎ記号でコマンドを分解する', () => {
  assert.deepEqual(splitSegments('ls && cat .env'), ['ls', 'cat .env']);
  assert.deepEqual(splitSegments('a ; b | c'), ['a', 'b', 'c']);
  assert.deepEqual(splitSegments('  ls  '), ['ls']);
});

test('接続情報ファイルの中身を見る操作を止める', () => {
  for (const cmd of [
    'cat backend/.env',
    'head -5 .env',
    'grep SECRET_KEY backend/.env',
    'ls && cat backend/.env',
  ]) {
    assert.equal(checkCommand(cmd).blocked, true, cmd);
  }
});

test('見本と、機密でない開発用の設定は止めない', () => {
  assert.equal(checkCommand('cat backend/.env.example').blocked, false);
  assert.equal(checkCommand('cat frontend/.env.development').blocked, false);
});

test('履歴の強制上書きを止める', () => {
  assert.equal(checkCommand('git push --force origin main').blocked, true);
  assert.equal(checkCommand('git push -f').blocked, true);
  assert.equal(checkCommand('git push --force-with-lease').blocked, true);
});

test('mainへの直接反映を止める', () => {
  assert.equal(checkCommand('git push origin main').blocked, true);
  assert.equal(checkCommand('git push origin HEAD:main').blocked, true);
  assert.equal(checkCommand('git push origin chore/foo').blocked, false);
});

test('プルリクエストの承認と取り込みを止める', () => {
  assert.equal(checkCommand('gh pr merge 12 --squash').blocked, true);
  assert.equal(checkCommand('gh pr review 12 --approve').blocked, true);
  assert.equal(checkCommand('gh pr view 12').blocked, false);
  assert.equal(checkCommand('gh pr create --fill').blocked, false);
});

test('データの入れ物ごと削除する操作を止める', () => {
  assert.equal(checkCommand('docker compose down -v').blocked, true);
  assert.equal(checkCommand('docker compose down --volumes').blocked, true);
  assert.equal(checkCommand('docker volume rm mytechpulse_db').blocked, true);
  assert.equal(checkCommand('docker compose down').blocked, false);
});

test('本番サーバーへの直接接続と手動公開を止める', () => {
  assert.equal(checkCommand('ssh ubuntu@54.168.29.67').blocked, true);
  assert.equal(checkCommand('npx wrangler pages deploy dist').blocked, true);
  assert.equal(checkCommand('curl https://api.mytechpulse.net/').blocked, false);
});

test('同じ行に見本ファイルがあっても、接続情報を見る操作は止める', () => {
  // 見本かどうかは行全体ではなく、ファイル1つずつで判定する必要がある
  assert.equal(checkCommand('cat backend/.env .env.example').blocked, true);
  assert.equal(checkCommand('cat .env.example backend/.env').blocked, true);
});

test('コンテナ越しに読む操作も止める', () => {
  assert.equal(checkCommand('docker compose exec api cat /app/.env').blocked, true);
});

test('入力の向きを変える書き方でも止める', () => {
  assert.equal(checkCommand('< backend/.env cat').blocked, true);
});

test('接続情報を記録対象に加える操作も止める', () => {
  assert.equal(checkCommand('git add backend/.env').blocked, true);
});

test('中身を見ない存在確認は止めない', () => {
  assert.equal(checkCommand('ls -la backend/.env').blocked, false);
  assert.equal(checkCommand('test -f backend/.env').blocked, false);
});

test('コマンドの前に飾りが付いていても止める', () => {
  assert.equal(checkCommand('sudo git push --force origin main').blocked, true);
  assert.equal(checkCommand('CI=1 git push origin main').blocked, true);
  assert.equal(checkCommand('gh --repo H4aruki/MyTechPulse pr merge 12').blocked, true);
});

test('止める理由の文章が付く', () => {
  const r = checkCommand('cat backend/.env');
  assert.equal(typeof r.message, 'string');
  assert.ok(r.message.length > 0);
});

test('空や未定義でも壊れない', () => {
  assert.equal(checkCommand('').blocked, false);
  assert.equal(checkCommand(undefined).blocked, false);
});
