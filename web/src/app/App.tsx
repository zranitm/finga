// The composition root (page 7): the screen flow (login, characters,
// world), the services, the Phaser canvas behind the screens, and the
// wiring between the socket and the stores. Features never import each
// other; they meet here.
import { useEffect, useMemo, useState } from 'react';

import { Modal } from '@/components/Modal';
import { LoginScreen } from '@/features/auth/LoginScreen';
import { CharactersScreen } from '@/features/characters/CharactersScreen';
import { ChatBox } from '@/features/chat/ChatBox';
import { ConnectionNotice } from '@/features/hud/ConnectionNotice';
import { DeathOverlay } from '@/features/hud/DeathOverlay';
import { Hud } from '@/features/hud/Hud';
import { PartyPanel } from '@/features/hud/PartyPanel';
import { Toasts } from '@/features/hud/Toasts';
import { InventoryWindow } from '@/features/inventory/InventoryWindow';
import { InviteToasts } from '@/features/party/InviteToast';
import { PartyWindow } from '@/features/party/PartyWindow';
import { SettingsPanel } from '@/features/settings/SettingsPanel';
import type { Character } from '@/net/rest';
import { character as characterStore } from '@/state/character';
import { connection } from '@/state/connection';
import { session } from '@/state/session';
import type { Account } from '@/state/session';
import { ui } from '@/state/ui';

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
                  name: welcome.character.name,
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
      socket={svc.socket}
      onLeave={() => {
        svc.game.leave();
        svc.socket.close();
        setPlaying(null);
      }}
    />
  );
}

/** Over the canvas: the HUD, the connection notice, and the way back to the characters. */
function WorldOverlay({
  socket,
  onLeave,
}: {
  readonly socket: Services['socket'];
  readonly onLeave: () => void;
}) {
  const inventoryOpen = ui.use((s) => s.windows.inventory);
  const partyOpen = ui.use((s) => s.windows.party);
  const me = characterStore.use((s) => s.character?.id ?? '');
  return (
    <>
      <Hud />
      <ConnectionNotice />
      <DeathOverlay />
      <Toasts />
      <PartyPanel />
      <InviteToasts socket={socket} />
      <ChatBox socket={socket} />
      {partyOpen && (
        <PartyWindow
          socket={socket}
          me={me}
          onClose={() => ui.getState().closeWindow('party')}
          onFocusChange={(focused) => ui.getState().setCaptureKeyboard(focused)}
        />
      )}
      {inventoryOpen && (
        <InventoryWindow
          socket={socket}
          onClose={() => ui.getState().closeWindow('inventory')}
          onFocusChange={(focused) => ui.getState().setCaptureKeyboard(focused)}
        />
      )}
      <div className="world-overlay" aria-label="World">
        <button type="button" className="btn btn-quiet" onClick={onLeave}>
          Back to characters
        </button>
      </div>
    </>
  );
}
