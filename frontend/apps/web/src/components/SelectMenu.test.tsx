import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';
import { SelectMenu } from './SelectMenu.js';

describe('SelectMenu', () => {
  it('renders a styled button trigger instead of a native select', () => {
    const markup = renderToStaticMarkup(
      <SelectMenu
        ariaLabel="模型"
        value="sonnet"
        options={[
          { value: 'opus', label: 'Opus' },
          { value: 'sonnet', label: 'Sonnet' },
        ]}
        onChange={() => {}}
      />,
    );

    expect(markup).toContain('Sonnet');
    expect(markup).toContain('aria-haspopup="listbox"');
    expect(markup).not.toContain('<select');
  });
});
