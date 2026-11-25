import type { ConfigDiffLine, ConfigFileDiff, ConfigHistory } from '../api/types';

/** Lines added and removed by a rollback. */
export function diffStats(lines: ConfigDiffLine[] | null): { added: number; removed: number } {
  let added = 0, removed = 0;
  for (const l of lines ?? []) {
    if (l.op === '+') added++;
    else if (l.op === '-') removed++;
  }
  return { added, removed };
}

/**
 * Shortens a diff to the changed lines with `context` unchanged lines around them;
 * longer unchanged runs become one `null` (rendered as "…").
 */
export function compactDiff(lines: ConfigDiffLine[], context = 2): (ConfigDiffLine | null)[] {
  const keep = lines.map(() => false);
  lines.forEach((l, i) => {
    if (l.op === ' ') return;
    for (let j = Math.max(0, i - context); j <= Math.min(lines.length - 1, i + context); j++) keep[j] = true;
  });
  const out: (ConfigDiffLine | null)[] = [];
  lines.forEach((l, i) => {
    if (keep[i]) out.push(l);
    else if (out.length === 0 || out[out.length - 1] !== null) out.push(null);
  });
  return out;
}

/** Files a rollback to the version would write. */
export const filesToRestore = (files: ConfigFileDiff[]) => files.filter((f) => !f.same);

/** Files changed outside the web UI since the newest version. */
export const driftedFiles = (h: ConfigHistory) => h.drift.filter((d) => d.state === 'modified');
