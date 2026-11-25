// The wardrobe (outfits.md section 7): the character in 3D, wearing what's tried on, beside a grid of every piece in each slot; Wear sends the collected picks, Reset goes back to what's worn.
import { useEffect, useState } from 'react';

import { Button } from '@/components/Button';
import { BesidePointer } from '@/features/shared/BesidePointer';
import { FigureView } from '@/features/shared/FigureView';
import { WorldWindow } from '@/features/shared/WorldWindow';
import { idempotencyKey } from '@/lib/id';
import type { Point } from '@/lib/placement';
import type { RestClient } from '@/net/rest';
import type { GameSocket } from '@/net/socket';
import { howToGet, pieceById, pieces, piecesFor } from '@/progression/pieces';
import type { Piece, Slot } from '@/progression/pieces';
import { character } from '@/state/character';
import { samePicks, wardrobe } from '@/state/wardrobe';
import type { Picks, Portrait } from '@/state/wardrobe';

import { loadWardrobe } from './load';

const TABS: readonly { readonly slot: Slot; readonly label: string }[] = [
  { slot: 'outfit', label: 'Outfit' },
  { slot: 'headwear', label: 'Headwear' },
  { slot: 'face', label: 'Face' },
  { slot: 'weapon', label: 'Weapon' },
  { slot: 'wings', label: 'Wings' },
];

export interface WardrobeProps {
  readonly socket: Pick<GameSocket, 'request'>;
  readonly rest: Pick<RestClient, 'getWardrobe'>;
  /** The figure drawn into canvas till it's closed. */
  readonly figureIn: (canvas: HTMLCanvasElement) => Portrait;
  readonly onClose: () => void;
}

/** look with the pieces collected doesn't hold put back to what's worn, slot by slot: what Wear sends. */
function wearable(look: Picks, worn: Picks, has: (id: string) => boolean): Picks {
  const keep = (slot: Exclude<Slot, 'headwear'>) =>
    look[slot] === '' || has(look[slot]) ? look[slot] : worn[slot];
  const head = look.headwear === '' || has(look.headwear);
  return {
    outfit: keep('outfit'),
    face: keep('face'),
    weapon: keep('weapon'),
    wings: keep('wings'),
    headwear: head ? look.headwear : worn.headwear,
    hideHeadwear: head ? look.hideHeadwear : worn.hideHeadwear,
  };
}

/** The piece the inventory's Preview handed the wardrobe, if any. */
function handed(): Piece | undefined {
  const id = wardrobe.getState().trying;
  return id ? pieceById(id) : undefined;
}

/** look with piece tried on in its slot, a hat showing; look itself for none. */
function tryOn(look: Picks, piece: Piece | undefined): Picks {
  if (!piece) {
    return look;
  }
  return {
    ...look,
    [piece.slot]: piece.id,
    ...(piece.slot === 'headwear' && { hideHeadwear: false }),
  };
}

/** The tile for a slot's first choices: the class's own look, a bare head, no wings. */
function OwnTile({
  label,
  picked,
  onPick,
}: {
  readonly label: string;
  readonly picked: boolean;
  readonly onPick: () => void;
}) {
  return (
    <button
      type="button"
      className="wardrobe-tile wardrobe-own collected"
      aria-pressed={picked}
      onClick={onPick}
    >
      {label}
    </button>
  );
}

/** A piece's card: its name and stars, its slot, a weapon look's kind, and how to get it. */
function PieceCard({ piece, collected }: { readonly piece: Piece; readonly collected: boolean }) {
  return (
    <div className="item-card piece-card">
      <strong className={`stars-${piece.stars}`}>{piece.name}</strong>
      <span className="item-stars">{'★'.repeat(piece.stars)}</span>
      <span className="muted">
        {TABS.find((t) => t.slot === piece.slot)?.label}
        {piece.kind && ` · ${piece.kind.replace('_', '-')}`}
      </span>
      <span className="muted">{collected ? 'In your wardrobe' : 'Not collected'}</span>
      <strong>How to get</strong>
      <ul>
        {howToGet(piece).map((line) => (
          <li key={line}>{line}</li>
        ))}
      </ul>
    </div>
  );
}

export function Wardrobe({ socket, rest, figureIn, onClose }: WardrobeProps) {
  const cls = character.use((s) => s.character?.class ?? '');
  const worn = wardrobe.use((s) => s.worn);
  const collected = wardrobe.use((s) => s.collected);
  const [tab, setTab] = useState<Slot>(() => (handed()?.slot as Slot | undefined) ?? 'outfit');
  const [look, setLook] = useState<Picks>(() => tryOn(worn, handed()));
  const [hovered, setHovered] = useState<{ piece: Piece; at: Point } | null>(null);
  const [asking, setAsking] = useState(false);
  const [refused, setRefused] = useState<string | null>(null);

  useEffect(
    () =>
      loadWardrobe(rest, () =>
        setRefused('Your wardrobe didn’t load. Close and open it to try again.'),
      ),
    [rest],
  );

  useEffect(() => {
    wardrobe.getState().preview(null); // a piece handed as the window opened is tried on already
    return wardrobe.subscribe(() => {
      const piece = handed();
      if (piece) {
        setTab(piece.slot as Slot);
        setLook((l) => tryOn(l, piece));
        wardrobe.getState().preview(null);
      }
    });
  }, []);

  const has = (id: string) => collected?.has(id) ?? false;
  const sends = wearable(look, worn, has);
  const pick = (next: Partial<Picks>) => {
    setRefused(null);
    setLook({ ...look, ...next });
  };
  const wear = () => {
    setAsking(true);
    setRefused(null);
    socket.request('setLook', { idempotencyKey: idempotencyKey(), look: sends }).then(
      () => setAsking(false),
      (err: unknown) => {
        setAsking(false);
        setRefused(err instanceof Error ? err.message : String(err));
      },
    );
  };
  const known = pieces.filter((p) => has(p.id)).length;
  const own =
    tab === 'headwear'
      ? [
          {
            label: 'Class look',
            picked: look.headwear === '' && !look.hideHeadwear,
            next: { headwear: '', hideHeadwear: false },
          },
          {
            label: 'Hidden',
            picked: look.headwear === '' && look.hideHeadwear,
            next: { headwear: '', hideHeadwear: true },
          },
        ]
      : [
          {
            label: tab === 'wings' ? 'None' : 'Class look',
            picked: look[tab] === '',
            next: { [tab]: '' },
          },
        ];

  return (
    <WorldWindow name="wardrobe" title="Wardrobe" onClose={onClose} initialX={300} initialY={90}>
      <div className="wardrobe">
        <FigureView
          figureIn={figureIn}
          cls={cls}
          look={look}
          label="Your character, dressed as picked"
          className="wardrobe-figure"
        />
        <div className="wardrobe-side">
          <div className="inventory-tabs" role="tablist">
            {TABS.map((t) => (
              <button
                key={t.slot}
                type="button"
                role="tab"
                aria-selected={t.slot === tab}
                className={`btn btn-quiet${t.slot === tab ? ' tab-active' : ''}`}
                onClick={() => setTab(t.slot)}
              >
                {t.label}
              </button>
            ))}
          </div>
          <div
            className="wardrobe-grid"
            role="tabpanel"
            aria-label={TABS.find((t) => t.slot === tab)?.label}
          >
            {own.map((o) => (
              <OwnTile
                key={o.label}
                label={o.label}
                picked={o.picked}
                onPick={() => pick(o.next)}
              />
            ))}
            {piecesFor(tab, cls).map((p) => (
              <button
                key={p.id}
                type="button"
                className={`wardrobe-tile stars-${p.stars} ${has(p.id) ? 'collected' : 'missing'}`}
                data-piece={p.id}
                aria-pressed={look[tab] === p.id}
                onClick={() => pick(tryOn(look, p))}
                onMouseEnter={(e) => setHovered({ piece: p, at: { x: e.clientX, y: e.clientY } })}
                onMouseMove={(e) => setHovered({ piece: p, at: { x: e.clientX, y: e.clientY } })}
                onMouseLeave={() => setHovered(null)}
              >
                {p.name}
              </button>
            ))}
          </div>
          <p className="muted wardrobe-count">
            {collected ? `Collected ${known} of ${pieces.length}` : 'Loading your wardrobe…'}
          </p>
          {!samePicks(look, sends) && (
            <p className="muted wardrobe-note">Pieces you haven’t collected stay as worn.</p>
          )}
          {refused && (
            <p className="wardrobe-refused" role="alert">
              {refused}
            </p>
          )}
          <div className="wardrobe-actions">
            <Button disabled={asking || samePicks(sends, worn)} onClick={wear}>
              Wear
            </Button>
            <Button
              variant="quiet"
              disabled={samePicks(look, worn)}
              onClick={() => {
                setRefused(null);
                setLook(worn);
              }}
            >
              Reset
            </Button>
          </div>
        </div>
      </div>
      {hovered && (
        <BesidePointer at={hovered.at}>
          <PieceCard piece={hovered.piece} collected={has(hovered.piece.id)} />
        </BesidePointer>
      )}
    </WorldWindow>
  );
}
