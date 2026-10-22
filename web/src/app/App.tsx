// The composition root (page 7): the screen flow (login, characters,
// world), the services, the Phaser canvas behind the screens, and the
// wiring between the socket and the stores. Features never import each
// other; they meet here.
import { useEffect, useMemo, useState } from 'react';

import { Modal } from '@/components/Modal';
import { LoginScreen } from '@/features/auth/LoginScreen';
import { CharactersScreen } from '@/features/characters/CharactersScreen';
import { SettingsPanel } from '@/features/settings/SettingsPanel';
import type { Character } from '@/net/rest';
import { character as characterStore } from '@/state/character';
import { connection } from '@/state/connection';
import { session } from '@/state/session';
import type { Account } from '@/state/session';

import { PhaserGame } from './PhaserGame';
import { createServices } from './services';
import type { Services } from './services';
import { leaveWorld, wireSocket } from './wiring';

export interface AppProps {
  /** Tests pass fakes; the real app builds them from the environment. */
  readonly services?: Services;
}

export function App({ services }: AppProps) {
  const svc = useMemo(() => services ?? createServices(), [services]);
  const loggedIn = session.use((s) => s.loggedIn);
  const account = session.use((s) => s.account);

  useEffect(() => wireSocket(svc.socket), [svc]);

  return (
    <>
      <PhaserGame handle={svc.game} />
      {!loggedIn || account === null ? (
        <LoginScreen
          rest={svc.rest}
          onLoggedIn={(username) => {
            connection.getState().setConnection('disconnected');
            session.getState().logIn({ id: username, username });
          }}
        />
      ) : (
        // Mounted only while logged in, so its screen state resets on log out or a kick.
        <LoggedIn svc={svc} account={account} />
      )}
    </>
  );
}

function LoggedIn({ svc, account }: { readonly svc: Services; readonly account: Account }) {
  const [playing, setPlaying] = useState<Character | null>(null);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [joinProblem, setJoinProblem] = useState<string | null>(null);

  // Leaving the logged-in screens for any reason (log out, kick) leaves the world too.
  useEffect(() => () => svc.game.leave(), [svc]);

  if (playing === null) {
    return (
      <>
        <CharactersScreen
          rest={svc.rest}
          username={account.username}
          onPlay={(c) => {
            setJoinProblem(null);
            setPlaying(c);
            svc.socket.connect(c.id).then(
              (welcome) => {
                svc.game.enter({
                  mapId: welcome.map_id,
                  characterId: welcome.character.id,
                  cls: welcome.character.class,
                });
              },
              (err: unknown) => {
                setJoinProblem(err instanceof Error ? err.message : 'Could not enter the world.');
                setPlaying(null);
              },
            );
          }}
          onLoggedOut={() => {
            leaveWorld();
          }}
          onSettings={() => setSettingsOpen(true)}
        />
        {settingsOpen && (
          <Modal title="Settings" onClose={() => setSettingsOpen(false)}>
            <SettingsPanel rest={svc.rest} onClose={() => setSettingsOpen(false)} />
          </Modal>
        )}
        {joinProblem && (
          <Modal title="Could not enter the world" onClose={() => setJoinProblem(null)}>
            <p>{joinProblem}</p>
          </Modal>
        )}
      </>
    );
  }

  return (
    <WorldOverlay
      onLeave={() => {
        svc.game.leave();
        svc.socket.close();
        setPlaying(null);
      }}
    />
  );
}

/** Over the canvas: the Welcome character and the connection, until the HUD (C09). */
function WorldOverlay({ onLeave }: { readonly onLeave: () => void }) {
  const c = characterStore.use((s) => s.character);
  const status = connection.use((s) => s.status);
  return (
    <div className="world-overlay" aria-label="World">
      <strong>{c ? `${c.name}, level ${c.level}` : 'Entering the world…'}</strong>
      <span>Connection: {status}</span>
      <button type="button" className="btn btn-quiet" onClick={onLeave}>
        Back to characters
      </button>
    </div>
  );
}
