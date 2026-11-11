// The sheet is the server's, the numbers combat uses; only a staged point's preview is worked out here.
import { useState } from 'react';
import type { DragEvent } from 'react';

import { Button } from '@/components/Button';
import { Modal } from '@/components/Modal';
import { carried, carry } from '@/features/shared/drag';
import { ItemDetails, useItemHover } from '@/features/shared/ItemHover';
import { initials, itemName, starClass, templateOf } from '@/features/shared/items';
import { useItemRequests } from '@/features/shared/useItemRequests';
import { WorldWindow } from '@/features/shared/WorldWindow';
import { classes } from '@/gamedata/classes';
import { equipSlots } from '@/gamedata/equip-slots';
import type { GameSocket } from '@/net/socket';
import {
  activeRoll,
  addStats,
  meets,
  noPoints,
  primaryKeys,
  sheetOf,
  total,
} from '@/progression/stats';
import type { DerivedKey, Primary, Sheet } from '@/progression/stats';
import type { CharacterView, StatSheet } from '@/protocol/messages';
import { character } from '@/state/character';
import { inventory } from '@/state/inventory';
import type { EquipSlotId, Equipment } from '@/state/inventory';

import { usePoints } from './usePoints';
import type { Points } from './usePoints';

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

type Show = (n: number) => string;
const percent: Show = (n) => `${num(n * 100)}%`;
const perSecond: Show = (n) => `${num(n)}/s`;

interface DerivedRow {
  readonly key: DerivedKey;
  readonly name: string;
  readonly show: Show;
}

// The sixteen in the four groups of Systems → Progression.
const groups: readonly { readonly name: string; readonly rows: readonly DerivedRow[] }[] = [
  {
    name: 'Offense',
    rows: [
      { key: 'physicalAttack', name: 'Physical attack', show: num },
      { key: 'magicAttack', name: 'Magic attack', show: num },
      { key: 'accuracy', name: 'Accuracy', show: num },
      { key: 'critRate', name: 'Crit rate', show: percent },
      { key: 'critDamage', name: 'Crit damage', show: percent },
      { key: 'penetration', name: 'Penetration', show: percent },
    ],
  },
  {
    name: 'Defense',
    rows: [
      { key: 'maxHp', name: 'Max HP', show: num },
      { key: 'physicalDefense', name: 'Physical defense', show: num },
      { key: 'magicDefense', name: 'Magic defense', show: num },
      { key: 'evasion', name: 'Evasion', show: num },
      { key: 'hpRegen', name: 'HP regen', show: perSecond },
    ],
  },
  {
    name: 'Resources',
    rows: [
      { key: 'maxMp', name: 'Max MP', show: num },
      { key: 'mpRegen', name: 'MP regen', show: perSecond },
    ],
  },
  {
    name: 'Speed',
    rows: [
      { key: 'attackSpeed', name: 'Attack speed', show: percent },
      { key: 'cooldownSpeed', name: 'Cooldown speed', show: percent },
      { key: 'moveSpeed', name: 'Move speed (combat maps)', show: percent },
    ],
  },
];

/** The equipment's part, shown only when it adds something. */
function Worn({ show, by }: { readonly show: Show; readonly by: number }) {
  return show(by) === show(0) ? null : <span className="stat-worn"> (+{show(by)})</span>;
}

function DerivedTable({
  group,
  sheet,
  preview,
}: {
  readonly group: (typeof groups)[number];
  readonly sheet: StatSheet | undefined;
  readonly preview: Sheet | null;
}) {
  return (
    <table className="character-stats" aria-label={group.name}>
      <caption>{group.name}</caption>
      <tbody>
        {group.rows.map(({ key, name, show }) => {
          const now = show(sheet?.derived?.[key] ?? 0);
          const then = preview ? show(preview.derived[key]) : now;
          return (
            <tr key={key}>
              <th scope="row">{name}</th>
              <td>
                {now}
                <Worn show={show} by={sheet?.derivedWorn?.[key] ?? 0} />
                {then !== now && <span className="stat-preview"> → {then}</span>}
              </td>
            </tr>
          );
        })}
      </tbody>
    </table>
  );
}

function PrimaryTable({
  sheet,
  points,
}: {
  readonly sheet: StatSheet | undefined;
  readonly points: Points;
}) {
  return (
    <table className="character-stats" aria-label="Primary stats">
      <tbody>
        {primaryKeys.map((k) => (
          <tr key={k}>
            <th scope="row">{k.toUpperCase()}</th>
            <td>
              {sheet?.primary?.[k] ?? 0}
              <Worn show={num} by={sheet?.primaryWorn?.[k] ?? 0} />
              {points.staged[k] > 0 && <span className="stat-preview"> +{points.staged[k]}</span>}
              {points.unspent > 0 && (
                <button
                  type="button"
                  className="stat-plus"
                  aria-label={`Add a point to ${k.toUpperCase()}`}
                  disabled={points.left === 0 || points.busy}
                  onClick={() => points.stage(k)}
                >
                  +
                </button>
              )}
            </td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

function PointsBar({ points, onReset }: { readonly points: Points; readonly onReset: () => void }) {
  const staged = total(points.staged) > 0;
  return (
    <div className="character-points" role="group" aria-label="Stat points">
      <span className="character-points-left">
        {points.unspent > 0 ? `Points: ${points.left}` : 'No points to spend'}
      </span>
      {staged && (
        <>
          <Button variant="quiet" disabled={points.busy} onClick={points.undo}>
            Undo
          </Button>
          <Button disabled={points.busy} onClick={points.apply}>
            Apply
          </Button>
        </>
      )}
      {points.unspent > 0 && !staged && (
        <Button variant="quiet" disabled={points.busy} onClick={points.autoAssign}>
          Auto-assign
        </Button>
      )}
      <Button
        variant="quiet"
        disabled={points.busy || points.spent === 0 || !points.freeResetLeft}
        title={points.freeResetLeft ? 'One free reset left' : 'The free reset is used'}
        onClick={onReset}
      >
        Reset
      </Button>
    </div>
  );
}

function ConfirmReset({
  spent,
  onReset,
  onClose,
}: {
  readonly spent: number;
  readonly onReset: () => void;
  readonly onClose: () => void;
}) {
  return (
    <Modal title="Reset stat points" onClose={onClose}>
      <p>
        All {spent} spent points go back to unspent. This uses your one free reset; later resets
        will cost.
      </p>
      <Button variant="danger" onClick={onReset}>
        Reset
      </Button>
      <Button variant="quiet" onClick={onClose}>
        Cancel
      </Button>
    </Modal>
  );
}

/** What the worn items give, with each template's requirements. */
function wornItems(equipment: Equipment) {
  return equipSlots.flatMap((s) => {
    const item = equipment[s.id];
    const t = item ? templateOf(item.template) : undefined;
    return item && t ? [{ slot: s.id, template: t, roll: item.stats ?? {} }] : [];
  });
}

/** The sheet the staged points would make, the gear they would switch on included; null with nothing staged. */
function previewSheet(c: CharacterView, equipment: Equipment, staged: Primary): Sheet | null {
  if (total(staged) === 0) {
    return null;
  }
  const own = addStats(c.stats ?? noPoints, staged);
  return sheetOf(c.class, c.level, own, activeRoll(wornItems(equipment), c.level, c.class, own));
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
  const { pending, notice, run, retry } = useItemRequests('character');
  const hover = useItemHover<EquipSlotId>();
  const points = usePoints(socket);
  const [confirmingReset, setConfirmingReset] = useState(false);
  const inactive = new Set(
    c
      ? wornItems(equipment)
          .filter((w) => !meets(w.template, c.level, c.class, c.stats ?? noPoints))
          .map((w) => w.slot)
      : [],
  );

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
        aria-label={
          item
            ? `${label}: ${itemName(item.template)}${inactive.has(slot.id) ? ', inactive' : ''}`
            : `${label}, empty`
        }
        aria-busy={p !== undefined}
        className={`slot equip-slot${item ? ` slot-equip ${starClass(item.template)}` : ''}${p ? ' slot-pending' : ''}${inactive.has(slot.id) ? ' slot-inactive' : ''}`}
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
  const preview = c ? previewSheet(c, equipment, points.staged) : null;
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
          <PointsBar points={points} onReset={() => setConfirmingReset(true)} />
          <div className="character-stat-tables">
            <PrimaryTable sheet={sheet} points={points} />
            <div className="character-derived">
              {groups.map((g) => (
                <DerivedTable key={g.name} group={g} sheet={sheet} preview={preview} />
              ))}
            </div>
          </div>
          {retryable && (
            <div className="inventory-detail">
              <span>{retryable[1].error}</span>
              <Button variant="quiet" onClick={() => retry(retryable[0])}>
                Retry
              </Button>
            </div>
          )}
          {(notice ?? points.notice) && (
            <p className="error" role="alert">
              {notice ?? points.notice}
            </p>
          )}
          {confirmingReset && (
            <ConfirmReset
              spent={points.spent}
              onClose={() => setConfirmingReset(false)}
              onReset={() => {
                setConfirmingReset(false);
                points.reset();
              }}
            />
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
