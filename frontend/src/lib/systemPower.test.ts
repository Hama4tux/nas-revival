import { describe, expect, it } from 'vitest';
import { confirmMatches, LATE_AFTER_MS, nextPhase } from './systemPower';

describe('typed confirmation', () => {
  it('needs the NAS name', () => {
    expect(confirmMatches(' NSA310 ', 'nsa310')).toBe(true);
    expect(confirmMatches('nsa31', 'nsa310')).toBe(false);
    expect(confirmMatches('', '')).toBe(false);
  });
});

describe('going down phases', () => {
  it('waits until the NAS stops answering', () => {
    expect(nextPhase('reboot', 'stopping', true, 5000)).toBe('stopping');
    expect(nextPhase('reboot', 'stopping', false, 5000)).toBe('offline');
  });
  it('reports a restart as back only after it was offline', () => {
    expect(nextPhase('reboot', 'offline', false, 60_000)).toBe('offline');
    expect(nextPhase('reboot', 'offline', true, 60_000)).toBe('back');
    expect(nextPhase('reboot', 'back', false, 60_000)).toBe('back');
  });
  it('says when a restart takes too long, and still notices it coming back', () => {
    expect(nextPhase('reboot', 'offline', false, LATE_AFTER_MS)).toBe('late');
    expect(nextPhase('reboot', 'late', true, LATE_AFTER_MS + 1)).toBe('back');
  });
  it('keeps a shut-down NAS offline', () => {
    expect(nextPhase('shutdown', 'offline', false, LATE_AFTER_MS * 2)).toBe('offline');
    expect(nextPhase('shutdown', 'offline', true, 60_000)).toBe('offline');
  });
});
