import { describe, expect, it } from 'vitest';
import { positionSelectMenu } from './selectMenuPosition.js';

describe('positionSelectMenu', () => {
  it('opens a bottom menu below the trigger without covering it', () => {
    expect(
      positionSelectMenu(
        { top: 80, bottom: 112, left: 300, right: 400, width: 100, height: 32 },
        { width: 160, height: 120 },
        { width: 800, height: 600 },
        'bottom',
      ),
    ).toEqual({ left: 300, top: 118 });
  });

  it('opens a top menu above a bottom-docked trigger', () => {
    expect(
      positionSelectMenu(
        { top: 520, bottom: 552, left: 120, right: 220, width: 100, height: 32 },
        { width: 160, height: 180 },
        { width: 800, height: 600 },
        'top',
      ),
    ).toEqual({ left: 120, top: 334 });
  });

  it('keeps the menu inside the viewport horizontally', () => {
    expect(
      positionSelectMenu(
        { top: 80, bottom: 112, left: 740, right: 790, width: 50, height: 32 },
        { width: 180, height: 120 },
        { width: 800, height: 600 },
        'bottom',
      ),
    ).toEqual({ left: 612, top: 118 });
  });
});
