import { useEffect, useState } from 'react';
import type { FormEvent } from 'react';

import { Button } from '@/components/Button';
import { useKeyboardCapture } from '@/features/shared/use-keyboard-capture';
import { RestError } from '@/net/rest';
import type { Character, RestClient } from '@/net/rest';

export const MAX_CHARACTERS = 3; // the Stage 1 cap per account (OD-09 default)
export const NAME_RULE = 'Name: 3 to 12 letters and digits, starting with a letter.';
export const stageClasses = [
  { id: 'warrior', name: 'Warrior' },
  { id: 'bowman', name: 'Bowman' },
] as const;

export function nameProblem(name: string): string | null {
  return /^[A-Za-z][A-Za-z0-9]{2,11}$/.test(name) ? null : NAME_RULE;
}

export interface CharactersScreenProps {
  readonly rest: RestClient;
  readonly username: string;
  readonly onPlay: (character: Character) => void;
  readonly onLoggedOut: () => void;
  readonly onSettings: () => void;
}

/** List, create and pick a character (A2); log out from here (A1). */
export function CharactersScreen({
  rest,
  username,
  onPlay,
  onLoggedOut,
  onSettings,
}: CharactersScreenProps) {
  const [characters, setCharacters] = useState<Character[] | null>(null);
  const [name, setName] = useState('');
  const [cls, setCls] = useState<string>('warrior');
  const [problem, setProblem] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const capture = useKeyboardCapture();

  useEffect(() => {
    let alive = true;
    rest
      .listCharacters()
      .then((list) => {
        if (alive) setCharacters(list);
      })
      .catch((err: unknown) => {
        if (alive)
          setProblem(err instanceof RestError ? err.message : 'Could not load characters.');
      });
    return () => {
      alive = false;
    };
  }, [rest]);

  const atCap = (characters?.length ?? 0) >= MAX_CHARACTERS;

  const create = async (e: FormEvent) => {
    e.preventDefault();
    const local = nameProblem(name);
    if (local) {
      setProblem(local);
      return;
    }
    setBusy(true);
    setProblem(null);
    try {
      const created = await rest.createCharacter(name, cls);
      setCharacters([...(characters ?? []), created]);
      setName('');
    } catch (err) {
      setProblem(err instanceof RestError ? err.message : 'Could not create the character.');
    } finally {
      setBusy(false);
    }
  };

  const logout = async () => {
    try {
      await rest.logout();
    } catch {
      // The token is cleared either way; the login screen is next.
    }
    onLoggedOut();
  };

  return (
    <div className="screen">
      <div className="panel">
        <header className="row">
          <h1>{username}&apos;s characters</h1>
          <Button variant="quiet" onClick={onSettings}>
            Settings
          </Button>
          <Button variant="quiet" onClick={() => void logout()}>
            Log out
          </Button>
        </header>
        {characters === null && !problem && <p>Loading…</p>}
        <ul className="characters" aria-label="Characters">
          {characters?.map((c) => (
            <li key={c.id}>
              <span>
                {c.name}, level {c.level} {c.class}
              </span>
              <Button onClick={() => onPlay(c)}>Play</Button>
            </li>
          ))}
        </ul>
        <form className="form" onSubmit={(e) => void create(e)} aria-label="Create a character">
          <label className="field">
            Name
            <input
              name="name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              disabled={atCap}
              {...capture}
            />
          </label>
          <fieldset className="field" disabled={atCap}>
            <legend>Class</legend>
            {stageClasses.map((option) => (
              <label key={option.id}>
                <input
                  type="radio"
                  name="class"
                  value={option.id}
                  checked={cls === option.id}
                  onChange={() => setCls(option.id)}
                />
                {option.name}
              </label>
            ))}
          </fieldset>
          {atCap && (
            <p className="notice">
              This account has the most characters allowed ({MAX_CHARACTERS}).
            </p>
          )}
          {problem && (
            <p className="error" role="alert">
              {problem}
            </p>
          )}
          <Button type="submit" disabled={busy || atCap}>
            Create
          </Button>
        </form>
      </div>
    </div>
  );
}
