// Each potion key drinks the potion that best fills what is missing, and waits for the answer before it drinks again.
import { RequestError } from '@/net/socket';
import { missing, pickPotion, restores } from '@/progression/potions';
import type { Restore, Vital, Vitals } from '@/progression/potions';
import { ERR, errorText } from '@/protocol/errors';
import type { ItemView } from '@/protocol/messages';

export interface PotionKeysDeps {
  /** null before Welcome. */
  readonly vitals: () => Vitals | null;
  readonly useTab: () => readonly (ItemView | null)[];
  /** Resolves once the world applied it. */
  readonly use: (potion: ItemView) => Promise<unknown>;
  readonly drank: (template: string, restored: Restore) => void;
  /** Why a key did nothing, or the world's refusal. */
  readonly notice: (text: string) => void;
}

export class PotionKeys {
  private readonly waiting = new Set<Vital>();

  constructor(private readonly deps: PotionKeysDeps) {}

  press(vital: Vital): void {
    const v = this.deps.vitals();
    if (!v || this.waiting.has(vital)) {
      return;
    }
    const need = missing(vital, v);
    if (need === 0) {
      this.deps.notice(errorText(vital === 'hp' ? ERR.HP_FULL : ERR.MP_FULL));
      return;
    }
    const potion = pickPotion(vital, need, this.deps.useTab());
    if (!potion) {
      this.deps.notice(`You have no ${vital.toUpperCase()} potions.`);
      return;
    }
    const restored = restores(potion.template, v);
    this.waiting.add(vital);
    this.deps
      .use(potion)
      .then(() => this.deps.drank(potion.template, restored))
      .catch((err: unknown) => {
        if (err instanceof RequestError && err.code !== 'disconnected' && err.code !== 'timeout') {
          this.deps.notice(err.message);
        }
      })
      .finally(() => this.waiting.delete(vital));
  }
}
