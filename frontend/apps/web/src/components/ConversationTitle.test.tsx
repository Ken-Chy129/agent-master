import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';
import { ConversationTitle } from './ConversationTitle.js';

describe('ConversationTitle', () => {
  it('renders only the conversation name', () => {
    const markup = renderToStaticMarkup(<ConversationTitle title="自动更新" />);

    expect(markup).toContain('自动更新');
    expect(markup).not.toContain('agent-master');
    expect(markup).not.toContain('<svg');
  });

  it('uses the generic conversation label when the title is empty', () => {
    expect(renderToStaticMarkup(<ConversationTitle title="" />)).toContain('会话');
  });
});
