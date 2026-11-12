// The composition root: features never import each other; they meet here.
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';

import { Modal } from '@/components/Modal';
import { LoginScreen } from '@/features/auth/LoginScreen';
import { CharacterWindow } from '@/features/character/CharacterWindow';
import { CharactersScreen } from '@/features/characters/CharactersScreen';
import { ChatBox } from '@/features/chat/ChatBox';
import { BossBar } from '@/features/hud/BossBar';
import { ConnectionNotice } from '@/features/hud/ConnectionNotice';
import { DeathOverlay } from '@/features/hud/DeathOverlay';
import { Hud } from '@/features/hud/Hud';
import { PartyPanel } from '@/features/hud/PartyPanel';
import { Toasts } from '@/features/hud/Toasts';
import { InventoryWindow } from '@/features/inventory/InventoryWindow';
import { NpcWindow } from '@/features/npc/NpcWindow';
import { InviteToasts } from '@/features/party/InviteToast';
import { PartyWindow } from '@/features/party/PartyWindow';
import { QuestLog } from '@/features/quests/QuestLog';
import { SettingsPanel } from '@/features/settings/SettingsPanel';
import { loadSettings } from '@/features/shared/settings';
import { watchSkillBar } from '@/features/skills/saveSkillBar';
import { SkillWindow } from '@/features/skills/SkillWindow';
import type { Character } from '@/net/rest';
import { character as characterStore } from '@/state/character';
import { connection } from '@/state/connection';
import { session } from '@/state/session';
import type { Account } from '@/state/session';
import { skillBar } from '@/state/skill-bar';
import { toasts } from '@/state/toasts';
import { ui } from '@/state/ui';

import { PhaserGame } from './PhaserGame';
import { createServices } from './services';
import type { Services } from './services';
import { leaveCharacter, leaveWorld, wireSocket } from './wiring';

export function App() {
  const svc = useMemo(() => createServices(), []);
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
  // The world's last saves on leaving, which the character list waits for.
  const saves = useRef<Promise<void>>(Promise.resolve());
  const afterSaves = useCallback(() => saves.current, []);
  const keepSaves = useCallback((pending: Promise<void>) => {
    saves.current = pending;
  }, []);

  // Leaving the logged-in screens for any reason (log out, kick) leaves the world too.
  useEffect(() => () => svc.game.leave(), [svc]);
  // Auto-assign acts on level-ups, so the account's settings are needed before play, not only in the panel.
  useEffect(() => {
    void loadSettings(svc.rest);
  }, [svc]);

  if (playing === null) {
    return (
      <>
        <CharactersScreen
          rest={svc.rest}
          username={account.username}
          onPlay={(c) => {
            setJoinProblem(null);
            setPlaying(c);
            skillBar.getState().load(c.skill_bar);
            svc.socket.connect(c.id).then(
              (welcome) => {
                const me = welcome.character ?? c; // always sent, but a message field is optional
                svc.game.enter({
                  mapId: welcome.mapId,
                  tickRateHz: welcome.tickRateHz,
                  characterId: me.id,
                  cls: me.class,
                  name: me.name,
                });
              },
              (err: unknown) => {
                console.info(
                  `finga: joining failed: ${err instanceof Error ? err.message : String(err)}`,
                );
                setJoinProblem('Try again in a moment.'); // the modal's title says what failed
                setPlaying(null);
              },
            );
          }}
          onLoggedOut={() => {
            leaveWorld();
          }}
          onSettings={() => setSettingsOpen(true)}
          afterSaves={afterSaves}
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
      rest={svc.rest}
      keepSaves={keepSaves}
      onLeave={() => {
        svc.game.leave();
        svc.socket.close();
        leaveCharacter();
        setPlaying(null);
      }}
    />
  );
}

function WorldOverlay({
  socket,
  rest,
  keepSaves,
  onLeave,
}: {
  readonly socket: Services['socket'];
  readonly rest: Services['rest'];
  /** Takes the saves still running when the world is left. */
  readonly keepSaves: (pending: Promise<void>) => void;
  readonly onLeave: () => void;
}) {
  const inventoryOpen = ui.use((s) => s.windows.includes('inventory'));
  const characterOpen = ui.use((s) => s.windows.includes('character'));
  const skillsOpen = ui.use((s) => s.windows.includes('skills'));
  const partyOpen = ui.use((s) => s.windows.includes('party'));
  const questsOpen = ui.use((s) => s.windows.includes('quests'));
  const npcOpen = ui.use((s) => s.windows.includes('npc'));
  const me = characterStore.use((s) => s.character?.id ?? '');
  // The next entry starts with no window open, whichever way this one ends.
  useEffect(() => () => ui.getState().clear(), []);
  useEffect(() => {
    if (me === '') {
      return undefined;
    }
    const stop = watchSkillBar(rest, me, (text) => toasts.getState().push(text));
    return () => keepSaves(stop());
  }, [rest, me, keepSaves]);
  return (
    <>
      <Hud />
      <ConnectionNotice />
      <BossBar />
      <DeathOverlay />
      <Toasts />
      <div className="world-corner">
        <button type="button" className="btn btn-quiet" onClick={onLeave}>
          Back to characters
        </button>
        <PartyPanel />
      </div>
      <InviteToasts socket={socket} />
      <ChatBox socket={socket} rest={rest} />
      <div className="windows">
        {partyOpen && (
          <PartyWindow socket={socket} me={me} onClose={() => ui.getState().closeWindow('party')} />
        )}
        {inventoryOpen && (
          <InventoryWindow socket={socket} onClose={() => ui.getState().closeWindow('inventory')} />
        )}
        {characterOpen && (
          <CharacterWindow socket={socket} onClose={() => ui.getState().closeWindow('character')} />
        )}
        {skillsOpen && (
          <SkillWindow socket={socket} onClose={() => ui.getState().closeWindow('skills')} />
        )}
        {questsOpen && (
          <QuestLog socket={socket} onClose={() => ui.getState().closeWindow('quests')} />
        )}
        {npcOpen && <NpcWindow socket={socket} onClose={() => ui.getState().closeWindow('npc')} />}
      </div>
    </>
  );
}
