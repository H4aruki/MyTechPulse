// 自動マージの時間帯の判定（Node標準のみ）。
// 使い方:
//   node guards.mjs in-blackout --now HH:MM [--rules file]   … 窓の中なら終了コード0、外なら1
//   node guards.mjs seconds-until-end --now HH:MM [--rules file] … 窓の終わりまでの秒数（窓の外なら0）
import { readFileSync } from "node:fs";
import { pathToFileURL } from "node:url";

export const DEFAULT_WINDOWS = [
  ["05:30", "06:20"],
  ["11:00", "11:50"],
  ["16:00", "16:50"],
  ["03:50", "04:20"],
];

export function toMinutes(hhmm) {
  const m = /^(\d{1,2}):(\d{2})$/.exec(String(hhmm));
  if (!m) throw new Error(`時刻の形式が正しくありません: ${hhmm}`);
  const h = Number(m[1]);
  const min = Number(m[2]);
  if (h > 23 || min > 59) throw new Error(`時刻の範囲が正しくありません: ${hhmm}`);
  return h * 60 + min;
}

// 開始以上・終了未満。終了が開始より前なら日付をまたぐ窓として扱う。
// 戻り値: 窓の中なら終わりまでの残り分数（1以上）、外なら0
export function minutesUntilEnd(now, windows = DEFAULT_WINDOWS) {
  const n = toMinutes(now);
  let best = 0;
  for (const [s, e] of windows) {
    const start = toMinutes(s);
    const end = toMinutes(e);
    let left = 0;
    if (start <= end) {
      if (n >= start && n < end) left = end - n;
    } else if (n >= start) {
      left = 1440 - n + end;
    } else if (n < end) {
      left = end - n;
    }
    if (left > best) best = left;
  }
  return best;
}

export function inBlackout(now, windows = DEFAULT_WINDOWS) {
  return minutesUntilEnd(now, windows) > 0;
}

export function secondsUntilEnd(now, windows = DEFAULT_WINDOWS) {
  return minutesUntilEnd(now, windows) * 60;
}

export function loadWindows(file) {
  if (!file) return DEFAULT_WINDOWS;
  const rules = JSON.parse(readFileSync(file, "utf8"));
  const w = rules?.mergeGuards?.blackoutWindowsJst;
  return Array.isArray(w) ? w : DEFAULT_WINDOWS;
}

function main(argv) {
  const [cmd, ...rest] = argv;
  const opt = {};
  for (let i = 0; i < rest.length; i += 2) opt[rest[i]] = rest[i + 1];
  if (!opt["--now"]) {
    console.error("--now HH:MM が必要です");
    return 2;
  }
  let windows;
  try {
    windows = loadWindows(opt["--rules"]);
    if (cmd === "in-blackout") return inBlackout(opt["--now"], windows) ? 0 : 1;
    if (cmd === "seconds-until-end") {
      console.log(secondsUntilEnd(opt["--now"], windows));
      return 0;
    }
  } catch (e) {
    console.error(e.message);
    return 2;
  }
  console.error("使い方: guards.mjs in-blackout|seconds-until-end --now HH:MM [--rules file]");
  return 2;
}

if (import.meta.url === pathToFileURL(process.argv[1] ?? "").href) {
  process.exit(main(process.argv.slice(2)));
}
