import userEvent from '@testing-library/user-event';

const fast = { delay: null } as const;

/** userEvent's direct calls with no timer between keys and clicks, which a busy machine stretched to seconds. */
export const user = {
  clear: (e: Element) => userEvent.clear(e),
  click: (e: Element) => userEvent.click(e, fast),
  dblClick: (e: Element) => userEvent.dblClick(e, fast),
  hover: (e: Element) => userEvent.hover(e, fast),
  unhover: (e: Element) => userEvent.unhover(e, fast),
  keyboard: (text: string) => userEvent.keyboard(text, fast),
  paste: (text: string) => userEvent.paste(text, fast),
  type: (e: Element, text: string) => userEvent.type(e, text, fast),
};
