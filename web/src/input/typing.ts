// Read from the element a key went to, never a flag or the focus by the time a listener runs: a handler before the game's may move the focus, as the chat does when it sends a line.

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

/** Whether el is a text field, so a key it gets belongs to it and not to the game. */
export function isTyping(el: EventTarget | null): boolean {
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
