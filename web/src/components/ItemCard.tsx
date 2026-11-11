// What one copy of an item is; the caller decides what to compare it with and what the character misses.
import { equipSlots } from '@/gamedata/equip-slots';
import { className } from '@/progression/classes';
import { primaryKeys } from '@/progression/stats';
import type { PrimaryKey } from '@/progression/stats';
import type { ItemView } from '@/protocol/messages';

import { itemStats, statText } from './item-stats';
import type { StatInfo, StatKey } from './item-stats';

/** An items.yaml template as the client generator writes it. */
export interface ItemTemplate {
  readonly id: string;
  readonly name: string;
  readonly tab: string;
  readonly slot?: string;
  readonly class?: string;
  readonly min_level?: number;
  /** The least of each own stat the item asks for. */
  readonly requires?: Readonly<Partial<Record<PrimaryKey, number>>>;
  readonly stars: number;
  readonly description: string;
  readonly stats?: Readonly<Record<string, readonly [number, number]>>;
  readonly heal_hp?: number;
  readonly heal_mp?: number;
}

export interface Unmet {
  readonly level: boolean;
  readonly class: boolean;
  /** The stats the character's own ones fall short of. */
  readonly stats: readonly PrimaryKey[];
}

export interface ItemCardProps {
  readonly template: ItemTemplate;
  readonly view: ItemView;
  /** The requirements the character misses, shown in red. */
  readonly unmet?: Unmet;
  /** Per stat, what wearing this copy instead of the worn one changes. */
  readonly changes?: Readonly<Partial<Record<StatKey, number>>>;
  /** A line above the name, such as "Worn". */
  readonly heading?: string;
}

const tabNames: Readonly<Record<string, string>> = { use: 'Use', etc: 'Etc' };

/** The slot kind's name for equipment, "Weapon" or "Ring"; the tab otherwise. */
function typeName(t: ItemTemplate): string {
  if (t.tab === 'equip') {
    return equipSlots.find((s) => s.kind === t.slot)?.name ?? 'Equipment';
  }
  return tabNames[t.tab] ?? t.tab;
}

function Change({ info, by }: { readonly info: StatInfo; readonly by: number | undefined }) {
  if (by === undefined || by === 0) {
    return null;
  }
  return (
    <span className={by > 0 ? 'stat-gain' : 'stat-loss'}>
      {' '}
      {by > 0 ? '+' : '−'}
      {statText(info, Math.abs(by))}
    </span>
  );
}

export function ItemCard({ template: t, view, unmet, changes, heading }: ItemCardProps) {
  const rows = itemStats.filter(
    (info) => t.stats?.[info.data] !== undefined || changes?.[info.key] !== undefined,
  );
  const forClass = t.class === undefined ? undefined : className(t.class);
  const required = primaryKeys.filter((k) => (t.requires?.[k] ?? 0) > 0);
  return (
    <div className="item-card" role="group" aria-label={heading ? `${heading}: ${t.name}` : t.name}>
      {heading && <div className="item-card-heading">{heading}</div>}
      <div className={`item-card-name stars-${t.stars}`}>{t.name}</div>
      <div className="item-card-type">
        <span aria-label={`${t.stars} stars`}>{'★'.repeat(t.stars)}</span> {typeName(t)}
      </div>
      {(t.min_level !== undefined || forClass !== undefined || required.length > 0) && (
        <ul className="item-card-requirements" aria-label="Requirements">
          {t.min_level !== undefined && (
            <li className={unmet?.level ? 'unmet' : undefined}>Level {t.min_level}</li>
          )}
          {forClass !== undefined && (
            <li className={unmet?.class ? 'unmet' : undefined}>{forClass}</li>
          )}
          {required.map((k) => (
            <li key={k} className={unmet?.stats.includes(k) ? 'unmet' : undefined}>
              {k.toUpperCase()} {t.requires?.[k]}
            </li>
          ))}
        </ul>
      )}
      {rows.length > 0 && (
        <ul className="item-card-stats" aria-label="Stats">
          {rows.map((info) => {
            const range = t.stats?.[info.data];
            return (
              <li key={info.key}>
                {info.label}
                {range && (
                  <>
                    {' '}
                    {statText(info, view.stats?.[info.key] ?? 0)}{' '}
                    <span className="stat-range">
                      ({statText(info, range[0])}–{statText(info, range[1])})
                    </span>
                  </>
                )}
                <Change info={info} by={changes?.[info.key]} />
              </li>
            );
          })}
        </ul>
      )}
      {t.heal_hp !== undefined && <div>Restores {t.heal_hp} HP</div>}
      {t.heal_mp !== undefined && <div>Restores {t.heal_mp} MP</div>}
      {view.quantity > 1 && <div>Quantity {view.quantity}</div>}
      <p className="item-card-description">{t.description}</p>
    </div>
  );
}

/** The second card of a comparison when the slot holds nothing. */
export function NothingWornCard() {
  return (
    <div className="item-card" role="group" aria-label="Worn: nothing">
      <div className="item-card-heading">Worn</div>
      <p className="item-card-description">Nothing is worn there.</p>
    </div>
  );
}
