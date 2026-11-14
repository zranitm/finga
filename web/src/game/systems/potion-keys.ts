// The potion key drinks the potion that best fills the HP missing, and waits for the answer before it drinks again.
import { RequestError } from '@/net/socket';
import { missing, pickPotion, restores } from '@/progression/potions';
import type { Vitals } from '@/progression/potions';
import { ERR, errorText } from '@/protocol/errors';
import type { ItemView } from '@/protocol/messages';

export interface PotionKeysDeps {
  /** null before Welcome. */
  readonly vitals: () => Vitals | null;
  readonly useTab: () => readonly (ItemView | null)[];
  /** Resolves once the world applied it. */
  readonly use: (potion: ItemView) => Promise<unknown>;
  /** The HP it restored, as worked out when it was asked. */
  readonly drank: (template: string, hp: number) => void;
  /** Why the key did nothing, or the world's refusal. */
  readonly notice: (text: string) => void;
}

export class PotionKeys {
  private waiting = false;

  constructor(private readonly deps: PotionKeysDeps) {}

  press(): void {
    const v = this.deps.vitals();
    if (!v || this.waiting) {
      return;
    }
    const need = missing(v);
    if (need === 0) {
      this.deps.notice(errorText(ERR.HP_FULL));
      return;
    }
    const potion = pickPotion(need, this.deps.useTab());
    if (!potion) {
      this.deps.notice('You have no HP potions.');
      return;
    }
    const restored = restores(potion.template, v);
    this.waiting = true;
    this.deps
      .use(potion)
      .then(() => this.deps.drank(potion.template, restored))
      .catch((err: unknown) => {
        if (err instanceof RequestError && err.code !== 'disconnected' && err.code !== 'timeout') {
          this.deps.notice(err.message);
        }
      })
      .finally(() => {
        this.waiting = false;
      });
  }
}
