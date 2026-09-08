#!/usr/bin/env node

import { readFileSync, readdirSync, statSync } from 'node:fs';
import { dirname, join, relative, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

function filesUnder(root, current = root) {
  const files = [];
  for (const entry of readdirSync(current, { withFileTypes: true })) {
    const path = join(current, entry.name);
    if (entry.isDirectory()) files.push(...filesUnder(root, path));
    else if (entry.isFile()) files.push(relative(root, path).replaceAll('\\', '/'));
  }
  return files;
}

export function compareSkillTrees(codexRoot, claudeRoot) {
  const codexFiles = filesUnder(codexRoot).sort();
  const claudeFiles = filesUnder(claudeRoot).sort();
  const allFiles = [...new Set([...codexFiles, ...claudeFiles])].sort();
  const errors = [];

  for (const file of allFiles) {
    const codexPath = join(codexRoot, file);
    const claudePath = join(claudeRoot, file);
    const codexExists = codexFiles.includes(file);
    const claudeExists = claudeFiles.includes(file);

    if (!codexExists || !claudeExists) {
      errors.push(`${file}: ${codexExists ? 'Claude側' : 'Codex側'}にありません`);
      continue;
    }

    if (!statSync(codexPath).isFile() || !statSync(claudePath).isFile()) continue;
    if (!readFileSync(codexPath).equals(readFileSync(claudePath))) {
      errors.push(`${file}: 内容が一致しません`);
    }
  }

  return errors;
}

const thisFile = fileURLToPath(import.meta.url);
if (resolve(process.argv[1] ?? '') === resolve(thisFile)) {
  const repoRoot = join(dirname(thisFile), '..', '..');
  const errors = compareSkillTrees(
    join(repoRoot, '.agents', 'skills'),
    join(repoRoot, '.claude', 'skills'),
  );

  if (errors.length > 0) {
    console.error('CodexとClaude CodeのSkillsに差があります:');
    for (const error of errors) console.error(`- ${error}`);
    process.exit(1);
  }

  console.log('CodexとClaude CodeのSkillsは一致しています。');
}
