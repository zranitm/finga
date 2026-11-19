// Whether a pointer reaches an element through global.css's pointer-events rules, which jsdom neither inherits nor hit-tests.
import { readFileSync } from 'node:fs';
import path from 'node:path';

interface Rule {
  readonly parts: readonly string[];
  readonly value: string;
  readonly order: number;
}

let loaded: readonly Rule[] | null = null;

function rules(): readonly Rule[] {
  if (loaded) {
    return loaded;
  }
  const style = document.createElement('style');
  style.textContent = readFileSync(path.resolve(process.cwd(), 'src/styles/global.css'), 'utf8');
  document.head.append(style);
  const out: Rule[] = [];
  const walk = (list: CSSRuleList) => {
    for (const r of Array.from(list)) {
      if (r instanceof CSSStyleRule) {
        const value = r.style.getPropertyValue('pointer-events');
        if (value) {
          out.push({ parts: r.selectorText.split(','), value, order: out.length });
        }
      } else if ('cssRules' in r) {
        walk((r as CSSGroupingRule).cssRules);
      }
    }
  };
  walk(style.sheet!.cssRules);
  loaded = out;
  return out;
}

/** A selector's weight as ids, then classes, attributes and pseudo-classes, then types. */
function specificity(selector: string): number {
  const s = selector.replace(/::[\w-]+/g, ' ');
  const ids = (s.match(/#[\w-]+/g) ?? []).length;
  const classes = (s.match(/\.[\w-]+|\[[^\]]+\]|:[\w-]+/g) ?? []).length;
  const types = (s.match(/(^|[\s>+~(])[a-z][\w-]*/gi) ?? []).length;
  return ids * 10000 + classes * 100 + types;
}

/** The pointer-events an element's own rules give it, if any. */
function declared(el: Element): string | undefined {
  const inline = el instanceof HTMLElement ? el.style.pointerEvents : '';
  if (inline) {
    return inline;
  }
  let best: { value: string; weight: number; order: number } | undefined;
  for (const r of rules()) {
    for (const part of r.parts) {
      const p = part.trim();
      if (p && el.matches(p)) {
        const weight = specificity(p);
        if (!best || weight > best.weight || (weight === best.weight && r.order > best.order)) {
          best = { value: r.value, weight, order: r.order };
        }
      }
    }
  }
  return best?.value;
}

/** Whether pointer-events, as inherited, lets a click or hover land on el. */
export function pointerReaches(el: Element): boolean {
  for (let at: Element | null = el; at; at = at.parentElement) {
    const value = declared(at);
    if (value && value !== 'inherit') {
      return value !== 'none';
    }
  }
  return true;
}
