import { describe, expect, it } from 'vitest';
import { composerSendSettlement } from './composerSend.js';

describe('composerSendSettlement', () => {
  it('releases image previews after a successful send', () => {
    expect(composerSendSettlement(true, 'session-a', 'session-a')).toEqual({
      releaseImages: true,
      restoreDraft: false,
    });
  });

  it('restores the draft after a failed send in the active session', () => {
    expect(composerSendSettlement(false, 'session-a', 'session-a')).toEqual({
      releaseImages: false,
      restoreDraft: true,
    });
  });

  it('keeps the failed draft stored without replacing another session', () => {
    expect(composerSendSettlement(false, 'session-a', 'session-b')).toEqual({
      releaseImages: false,
      restoreDraft: false,
    });
  });

  it('does not restore a draft when no target session exists', () => {
    expect(composerSendSettlement(false, null, null)).toEqual({
      releaseImages: false,
      restoreDraft: false,
    });
  });
});
