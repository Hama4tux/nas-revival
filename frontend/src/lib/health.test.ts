import { describe, expect, it } from 'vitest';
import type { Overview, SystemHealth } from '../api/types';
import { deriveHealth } from './health';

const withHealth = (health: SystemHealth) => ({ health }) as Overview;
const ok: SystemHealth = { status: 'ok', checkedAt: '2026-11-25T16:00:00Z', smartCheckedAt: null, problems: [] };

describe('deriveHealth', () => {
  it('is ok for a healthy system and keeps checkedAt', () => {
    expect(deriveHealth(withHealth(ok), false)).toEqual({ level: 'ok', reasons: [], checkedAt: ok.checkedAt });
  });
  it('maps server severities and subjects', () => {
    const h = deriveHealth(withHealth({
      ...ok, status: 'crit',
      problems: [{ severity: 'crit', code: 'fsCritical', subject: '/' }, { severity: 'warn', code: 'fanStopped', subject: 'Ventilátor' }],
    }), false);
    expect(h.level).toBe('problem');
    expect(h.reasons).toEqual([
      { level: 'problem', code: 'fsCritical', arg: '/' },
      { level: 'warning', code: 'fanStopped', arg: 'Ventilátor' },
    ]);
  });
  it('warn is warning, unknown stays unknown', () => {
    expect(deriveHealth(withHealth({ ...ok, status: 'warn', problems: [{ severity: 'warn', code: 'fsWarn', subject: '/' }] }), false).level).toBe('warning');
    expect(deriveHealth(withHealth({ ...ok, status: 'unknown', problems: [{ severity: 'warn', code: 'collectFailed' }] }), false).level).toBe('unknown');
  });
  it('stale data turns ok into a warning and is listed last', () => {
    const h = deriveHealth(withHealth(ok), true);
    expect(h.level).toBe('warning');
    expect(h.reasons).toEqual([{ level: 'warning', code: 'stale' }]);
    const crit = deriveHealth(withHealth({ ...ok, status: 'crit', problems: [{ severity: 'crit', code: 'serviceFailed', subject: 'SSH' }] }), true);
    expect(crit.level).toBe('problem');
    expect(crit.reasons.map((r) => r.code)).toEqual(['serviceFailed', 'stale']);
  });
});
