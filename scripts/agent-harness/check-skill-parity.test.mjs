import { test } from 'node:test';
import assert from 'node:assert/strict';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { compareSkillTrees } from './check-skill-parity.mjs';

const repoRoot = join(dirname(fileURLToPath(import.meta.url)), '..', '..');

test('CodexとClaude CodeのSkillsが同じ内容である', () => {
  const errors = compareSkillTrees(
    join(repoRoot, '.agents', 'skills'),
    join(repoRoot, '.claude', 'skills'),
  );
  assert.deepEqual(errors, []);
});
