import { useEffect, useState } from 'react';
import type { FormEvent } from 'react';

import { Button } from '@/components/Button';
import { RestError } from '@/net/rest';
import type { Character, RestClient } from '@/net/rest';

export const MAX_CHARACTERS = 3; // per account
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
  // The list failed to load, as while the gateway restarts; Try again loads it once more.
  const [loadProblem, setLoadProblem] = useState<string | null>(null);
  const [loads, setLoads] = useState(0);
  const [busy, setBusy] = useState(false);
  const [leaving, setLeaving] = useState(false); // a log out is on its way, so the log out and Play buttons wait

  useEffect(() => {
    let alive = true;
    rest
      .listCharacters()
      .then((list) => {
        if (alive) setCharacters(list);
      })
      .catch((err: unknown) => {
        if (alive)
          setLoadProblem(err instanceof RestError ? err.message : 'Could not load characters.');
      });
    return () => {
      alive = false;
    };
  }, [rest, loads]);

  const reload = () => {
    setLoadProblem(null);
    setLoads((n) => n + 1);
  };

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
    setLeaving(true);
    try {
      await rest.logout();
    } catch {
      // The token is cleared either way; the login screen is next.
    }
    onLoggedOut();
  };

  const logoutEverywhere = async () => {
    setLeaving(true);
    setProblem(null);
    try {
      await rest.logoutEverywhere();
    } catch (err) {
      // A 401 means this session had already ended; any other failure may leave the others open.
      if (!(err instanceof RestError && err.status === 401)) {
        setProblem(err instanceof RestError ? err.message : 'Could not log out everywhere.');
        setLeaving(false);
        return;
      }
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
          <Button variant="quiet" onClick={() => void logout()} disabled={leaving}>
            Log out
          </Button>
          <Button variant="quiet" onClick={() => void logoutEverywhere()} disabled={leaving}>
            Log out everywhere
          </Button>
        </header>
        {characters === null && !loadProblem && <p>Loading…</p>}
        {loadProblem && (
          <div className="row">
            <p className="error" role="alert">
              {loadProblem}
            </p>
            <Button variant="quiet" onClick={reload}>
              Try again
            </Button>
          </div>
        )}
        <ul className="characters" aria-label="Characters">
          {characters?.map((c) => (
            <li key={c.id}>
              <span>
                {c.name}, level {c.level} {c.class}
              </span>
              <Button onClick={() => onPlay(c)} disabled={leaving}>
                Play
              </Button>
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
