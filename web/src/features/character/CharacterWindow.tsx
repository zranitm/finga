// Shows only what the server sent: the sheet is the numbers combat uses, and InventoryChanged moves what is worn.
import type { DragEvent } from 'react';

import { Button } from '@/components/Button';
import { carried, carry } from '@/features/shared/drag';
import { ItemDetails, useItemHover } from '@/features/shared/ItemHover';
import { initials, itemName, starClass } from '@/features/shared/items';
import { useItemRequests } from '@/features/shared/useItemRequests';
import { WorldWindow } from '@/features/shared/WorldWindow';
import { classes } from '@/gamedata/classes';
import { equipSlots } from '@/gamedata/equip-slots';
import type { GameSocket } from '@/net/socket';
import type { CharacterView, DerivedStats, Stats } from '@/protocol/messages';
import { character } from '@/state/character';
import { inventory } from '@/state/inventory';
import type { EquipSlotId } from '@/state/inventory';

export interface CharacterWindowProps {
  readonly socket: Pick<GameSocket, 'request'>;
  readonly onClose: () => void;
}

type Slot = (typeof equipSlots)[number];

/** A slot's name, numbered when two slots share it: Ring 1, Ring 2. */
function slotLabel(slot: Slot): string {
  const same = equipSlots.filter((s) => s.name === slot.name);
  return same.length > 1 ? `${slot.name} ${same.indexOf(slot) + 1}` : slot.name;
}

// The figure stands between the slots: the weapon and the armor on its left, the shoes and the accessories on its right.
const left = equipSlots.slice(0, 5);
const right = equipSlots.slice(5);

/** Two decimals at most, so 8.75 stays 8.75 and 30 reads 30. */
function num(n: number): string {
  return String(Math.round(n * 100) / 100);
}

interface Row {
  readonly name: string;
  readonly total: string;
  /** The equipment's part, shown only when it adds something. */
  readonly worn: string | null;
}

function row(name: string, total: number, worn: number, show: (n: number) => string): Row {
  return { name, total: show(total), worn: show(worn) === show(0) ? null : show(worn) };
}

const percent = (n: number) => `${num(n * 100)}%`;

const primaryRows = (total: Stats | undefined, worn: Stats | undefined): Row[] =>
  (['str', 'dex', 'int', 'agi', 'vit'] as const).map((k) =>
    row(k.toUpperCase(), total?.[k] ?? 0, worn?.[k] ?? 0, num),
  );

const derivedRows = (total: DerivedStats | undefined, worn: DerivedStats | undefined): Row[] => [
  row('Attack', total?.physicalAttack ?? 0, worn?.physicalAttack ?? 0, num),
  row('Defense', total?.physicalDefense ?? 0, worn?.physicalDefense ?? 0, num),
  row('Accuracy', total?.accuracy ?? 0, worn?.accuracy ?? 0, num),
  row('Evasion', total?.evasion ?? 0, worn?.evasion ?? 0, num),
  row('Crit rate', total?.critRate ?? 0, worn?.critRate ?? 0, percent),
  row('Crit damage', total?.critDamage ?? 0, worn?.critDamage ?? 0, percent),
];

function StatTable({ label, rows }: { readonly label: string; readonly rows: readonly Row[] }) {
  return (
    <table className="character-stats" aria-label={label}>
      <tbody>
        {rows.map((r) => (
          <tr key={r.name}>
            <th scope="row">{r.name}</th>
            <td>
              {r.total}
              {r.worn !== null && <span className="stat-worn"> (+{r.worn})</span>}
            </td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

function Vitals({
  c,
  expToNext,
}: {
  readonly c: CharacterView;
  readonly expToNext: number | null;
}) {
  return (
    <dl className="character-vitals">
      <dt>EXP</dt>
      <dd>{expToNext === null ? 'MAX' : `${c.exp} / ${expToNext}`}</dd>
      <dt>HP</dt>
      <dd>
        {c.hp} / {c.maxHp}
      </dd>
      <dt>MP</dt>
      <dd>
        {c.mp} / {c.maxMp}
      </dd>
    </dl>
  );
}

export function CharacterWindow({ socket, onClose }: CharacterWindowProps) {
  const c = character.use((s) => s.character);
  const expToNext = character.use((s) => s.expToNext);
  const equipment = inventory.use((s) => s.equipment);
  const { pending, notice, run, retry } = useItemRequests();
  const hover = useItemHover<EquipSlotId>();

  const unequip = (slot: EquipSlotId) => {
    if (equipment[slot] && !pending.has(slot)) {
      run(slot, (key) => socket.request('unequip', { idempotencyKey: key, equipSlot: slot }));
    }
  };

  /** An item dropped on a slot asks for that slot; the server refuses one that doesn't go there. */
  const onDrop = (e: DragEvent, slot: EquipSlotId) => {
    e.preventDefault();
    const d = carried(e);
    if (d?.from === 'inventory' && !pending.has(slot)) {
      run(slot, (key) =>
        socket.request('equip', { idempotencyKey: key, itemId: d.itemId, equipSlot: slot }),
      );
    }
  };

  const slotButton = (slot: Slot) => {
    const item = equipment[slot.id];
    const p = pending.get(slot.id);
    const label = slotLabel(slot);
    return (
      <button
        key={slot.id}
        type="button"
        aria-label={item ? `${label}: ${itemName(item.template)}` : `${label}, empty`}
        aria-busy={p !== undefined}
        className={`slot equip-slot${item ? ` slot-equip ${starClass(item.template)}` : ''}${p ? ' slot-pending' : ''}`}
        draggable={item !== null && p === undefined}
        {...hover.bind(slot.id, item, false)}
        onDragStart={(e) => {
          hover.clear();
          carry(e, { from: 'worn', equipSlot: slot.id });
        }}
        onDragOver={(e) => e.preventDefault()}
        onDrop={(e) => onDrop(e, slot.id)}
        onDoubleClick={() => unequip(slot.id)}
      >
        {item ? (
          <span className="slot-icon" aria-hidden="true">
            {initials(item.template)}
          </span>
        ) : (
          <span className="equip-slot-name" aria-hidden="true">
            {label}
          </span>
        )}
      </button>
    );
  };

  const retryable = [...pending.entries()].find(([, p]) => p.retryable);
  const sheet = c?.sheet;
  return (
    <WorldWindow name="character" title="Character" onClose={onClose} initialX={24} initialY={80}>
      {c && (
        <div className="character">
          <header className="character-header">
            <span className="character-name">{c.name}</span>
            <span>
              {classes.find((k) => k.id === c.class)?.name ?? c.class} · Lv {c.level}
            </span>
          </header>
          <Vitals c={c} expToNext={expToNext} />
          <div className="character-equipment" role="group" aria-label="Equipment">
            <div className="equip-column">{left.map(slotButton)}</div>
            <div className="character-figure" aria-hidden="true" />
            <div className="equip-column">{right.map(slotButton)}</div>
          </div>
          <div className="character-stat-tables">
            <StatTable
              label="Primary stats"
              rows={primaryRows(sheet?.primary ?? c.stats, sheet?.primaryWorn)}
            />
            <StatTable
              label="Derived stats"
              rows={derivedRows(sheet?.derived, sheet?.derivedWorn)}
            />
          </div>
          {retryable && (
            <div className="inventory-detail">
              <span>{retryable[1].error}</span>
              <Button variant="quiet" onClick={() => retry(retryable[0])}>
                Retry
              </Button>
            </div>
          )}
          {notice && (
            <p className="error" role="alert">
              {notice}
            </p>
          )}
        </div>
      )}
      <ItemDetails
        hovered={hover.hovered}
        item={hover.hovered ? equipment[hover.hovered.key] : null}
      />
    </WorldWindow>
  );
}
