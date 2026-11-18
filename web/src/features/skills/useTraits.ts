// Choosing a skill's trait (skill-traits.md section 7): sent at once, with no staging; it stays pending until the world answers and, once it agrees, until the sheet carries the change, so the diamond never flicks back.
import { useState } from 'react';

import type { GameSocket } from '@/net/socket';
import { RequestError } from '@/net/socket';
import { pointsIn, traitOpen } from '@/progression/skills';
import type { Skill } from '@/progression/skills';
import { character } from '@/state/character';

export interface TraitChoice {
  /** The trait id a skill is used with, as the world resolves it: the sheet's extra while it's open, else the default's. */
  readonly chosen: (skill: Skill) => string;
  /** The choice on its way, if any. */
  readonly pending: Pending | null;
  readonly notice: string | null;
  /** Sends a choice of another trait; the window lets one be on its way at a time. */
  readonly choose: (skill: Skill, trait: string) => void;
}

interface Pending {
  readonly skill: string;
  readonly trait: string;
}

/** A choice sent, and once the world agreed, the sheet it agreed over. */
interface Sent {
  readonly skill: Skill;
  readonly trait: string;
  readonly over?: unknown;
}

const none: Readonly<Record<string, string>> = {};
const noPoints: Readonly<Record<string, number>> = {};

export function useTraits(socket: Pick<GameSocket, 'request'>): TraitChoice {
  const sheet = character.use((s) => s.character?.sheet);
  const traits = sheet?.skills?.traits ?? none;
  const invested = sheet?.skills?.invested ?? noPoints;
  const [sent, setSent] = useState<Sent | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const chosen = (skill: Skill) =>
    skill.traits[1]?.id === traits[skill.id] && traitOpen(skill, pointsIn(skill, invested))
      ? traits[skill.id]!
      : (skill.traits[0]?.id ?? '');
  const pending =
    sent && ('over' in sent ? sent.over === sheet : true) && chosen(sent.skill) !== sent.trait
      ? { skill: sent.skill.id, trait: sent.trait }
      : null;
  const choose = (skill: Skill, trait: string) => {
    if (chosen(skill) === trait) {
      return;
    }
    const mine: Sent = { skill, trait };
    setSent(mine);
    setNotice(null);
    socket.request('chooseTrait', { skill: skill.id, trait }).then(
      () => {
        setSent((was) =>
          was === mine ? { ...mine, over: character.getState().character?.sheet } : was,
        );
      },
      (err: unknown) => {
        setSent((was) => (was === mine ? null : was));
        setNotice(
          err instanceof RequestError && (err.code === 'timeout' || err.code === 'disconnected')
            ? 'No answer yet. Try again.'
            : err instanceof Error
              ? err.message
              : 'The server refused.',
        );
      },
    );
  };
  return { chosen, pending, notice, choose };
}
