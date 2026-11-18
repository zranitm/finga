import { describe, expect, it } from 'vitest';

import { anyModal, openModal } from './modals';

describe('modals', () => {
  it('knows whether one is open and which is newest, an older one closing first leaving the newest so', () => {
    expect(anyModal()).toBe(false);
    const settings = openModal();
    const confirm = openModal();
    expect([anyModal(), settings.newest(), confirm.newest()]).toEqual([true, false, true]);
    settings.close();
    settings.close(); // twice is once
    expect([anyModal(), confirm.newest()]).toEqual([true, true]);
    confirm.close();
    expect(anyModal()).toBe(false);
  });
});
