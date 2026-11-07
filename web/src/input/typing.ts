// Read from the focus itself, never a flag, so it cannot go stale when a focused element goes without a blur.

/** Input types that take no typed text; Space and arrows on them belong to the game. */
const untyped = new Set([
  'button',
  'checkbox',
  'color',
  'file',
  'hidden',
  'image',
  'radio',
  'range',
  'reset',
  'submit',
]);

/** Whether a text field has focus, so keys belong to it and not to the game. */
export function isTyping(): boolean {
  const el = document.activeElement;
  if (!(el instanceof HTMLElement)) {
    return false;
  }
  if (el instanceof HTMLInputElement) {
    return !untyped.has(el.type);
  }
  if (el instanceof HTMLTextAreaElement || el instanceof HTMLSelectElement) {
    return true;
  }
  const editable = el.closest('[contenteditable]'); // the nearest says, so a "false" inside an editable region wins
  return editable !== null && editable.getAttribute('contenteditable') !== 'false';
}
