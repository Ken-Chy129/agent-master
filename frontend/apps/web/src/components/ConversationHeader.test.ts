import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';

const source = readFileSync(new URL('./Conversation.tsx', import.meta.url), 'utf8');

describe('conversation header', () => {
  it('does not duplicate model metadata beside the conversation title', () => {
    expect(source).not.toMatch(/currentSessionMeta\??\.model/);
  });
});
