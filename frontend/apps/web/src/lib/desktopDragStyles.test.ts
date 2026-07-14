import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';

const styles = readFileSync(new URL('../styles.css', import.meta.url), 'utf8');

describe('macOS desktop drag regions', () => {
  it('keeps descendants inside a drag handle draggable', () => {
    expect(styles).toMatch(
      /\.desktop-mac \.app-drag,\s*\.desktop-mac \.app-drag \*\s*\{\s*-webkit-app-region:\s*drag;/,
    );
  });

  it('keeps complete interactive subtrees clickable', () => {
    const noDragRule = styles.match(
      /\.desktop-mac \.app-drag button,[\s\S]*?\{\s*-webkit-app-region:\s*no-drag;/,
    )?.[0];

    expect(noDragRule).toBeDefined();
    expect(noDragRule).toMatch(/\.desktop-mac \.app-drag button \*/);
    expect(noDragRule).toMatch(/\.desktop-mac \.app-drag form \*/);
    expect(noDragRule).toMatch(/\.desktop-mac \.app-drag a \*/);
    expect(noDragRule).toMatch(/\.desktop-mac \.app-drag \[role=['"]button['"]\] \*/);
  });
});
