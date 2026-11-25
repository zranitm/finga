// The composition root: features never import each other; they meet here.
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';

import { Modal } from '@/components/Modal';
import { LoginScreen } from '@/features/auth/LoginScreen';
import { CharacterWindow } from '@/features/character/CharacterWindow';
import { CharactersScreen } from '@/features/characters/CharactersScreen';
import { ChatBox } from '@/features/chat/ChatBox';
import { ConnectionNotice } from '@/features/hud/ConnectionNotice';
import { DeathOverlay } from '@/features/hud/DeathOverlay';
import { Hud } from '@/features/hud/Hud';
import { MatchBar, MatchCount } from '@/features/hud/MatchBar';
import { PartyPanel } from '@/features/hud/PartyPanel';
import { TopBars } from '@/features/hud/TargetFrame';
import { TeleportBar } from '@/features/hud/TeleportBar';
import { Toasts } from '@/features/hud/Toasts';
import { InventoryWindow } from '@/features/inventory/InventoryWindow';
import { LootWindow } from '@/features/loot/LootWindow';
import { MapWindow } from '@/features/map/MapWindow';
import { Minimap } from '@/features/map/Minimap';
import { NpcWindow } from '@/features/npc/NpcWindow';
import { InviteToasts } from '@/features/party/InviteToast';
import { PartyWindow } from '@/features/party/PartyWindow';
import { ArenaWindow } from '@/features/pvp/ArenaWindow';
import { boardReader } from '@/features/pvp/boards';
import { EnemyFrames, TeamFrames } from '@/features/pvp/MatchFrames';
import { MatchOffer } from '@/features/pvp/MatchOffer';
import { MatchResult } from '@/features/pvp/MatchResult';
import { QueueTag } from '@/features/pvp/QueueTag';
import { watchPvpProfile } from '@/features/pvp/watchProfile';
import { QuestLog } from '@/features/quests/QuestLog';
import { SettingsPanel } from '@/features/settings/SettingsPanel';
import { loadSettings, watchPlaySettings } from '@/features/shared/settings';
import { watchArenaBuild } from '@/features/skills/saveArenaBuild';
import { watchSkillBar } from '@/features/skills/saveSkillBar';
import { SkillWindow } from '@/features/skills/SkillWindow';
import { loadWardrobe } from '@/features/wardrobe/load';
import { Wardrobe } from '@/features/wardrobe/Wardrobe';
import type { Character } from '@/net/rest';
import { ERR, errorText } from '@/protocol/errors';
import { arenaBuild } from '@/state/arena-build';
import { character as characterStore } from '@/state/character';
import { connection } from '@/state/connection';
import { pvpProfile } from '@/state/pvp-profile';
import { session } from '@/state/session';
import type { Account } from '@/state/session';
import { skillBar } from '@/state/skill-bar';
import { toasts } from '@/state/toasts';
import { ui } from '@/state/ui';
import type { Portrait } from '@/state/wardrobe';

import { GameMount } from './GameMount';
import { watchKeyboardLayout } from './keyboard-layout';
import { layoutOf } from './layouts';
import { createServices } from './services';
import type { Services } from './services';
import { leaveCharacter, leaveWorld, wireSocket } from './wiring';

export function App() {
  const svc = useMemo(() => createServices(), []);
  const loggedIn = session.use((s) => s.loggedIn);
  const account = session.use((s) => s.account);

  useEffect(() => wireSocket(svc.socket), [svc]);
  useEffect(() => watchKeyboardLayout(), []);

  return (
    <>
      <GameMount view={svc.game} />
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
  const figureIn = useCallback((canvas: HTMLCanvasElement) => svc.game.portrait(canvas), [svc]); // one function, so a window's figure lives as long as the window
  const [playing, setPlaying] = useState<Character | null>(null);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [joinProblem, setJoinProblem] = useState<string | null>(null);
  // The world's last saves on leaving, every one of them, which the character list waits for.
  const saves = useRef<Promise<void>>(Promise.resolve());
  const afterSaves = useCallback(() => saves.current, []);
  const keepSaves = useCallback((pending: Promise<void>) => {
    saves.current = Promise.all([saves.current, pending]).then(() => undefined);
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
            arenaBuild.reset(); // the profile brings this character's
            pvpProfile.reset();
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
                svc.socket.close(); // it would otherwise retry, and a welcome then would enter the world unseen
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
      figureIn={figureIn}
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
  figureIn,
  keepSaves,
  onLeave,
}: {
  readonly socket: Services['socket'];
  readonly rest: Services['rest'];
  /** A character drawn into a window's canvas, as the game draws one. */
  readonly figureIn: (canvas: HTMLCanvasElement) => Portrait;
  /** Takes each of the saves still running when the world is left. */
  readonly keepSaves: (pending: Promise<void>) => void;
  readonly onLeave: () => void;
}) {
  const inventoryOpen = ui.use((s) => s.windows.includes('inventory'));
  const characterOpen = ui.use((s) => s.windows.includes('character'));
  const skillsOpen = ui.use((s) => s.windows.includes('skills'));
  const partyOpen = ui.use((s) => s.windows.includes('party'));
  const questsOpen = ui.use((s) => s.windows.includes('quests'));
  const mapOpen = ui.use((s) => s.windows.includes('map'));
  const arenaOpen = ui.use((s) => s.windows.includes('arena'));
  const npcOpen = ui.use((s) => s.windows.includes('npc'));
  const lootOpen = ui.use((s) => s.windows.includes('loot'));
  const wardrobeOpen = ui.use((s) => s.windows.includes('wardrobe'));
  const me = characterStore.use((s) => s.character?.id ?? '');
  const board = useMemo(() => boardReader(rest), [rest]); // kept while the window closes and opens
  const [settingsOpen, setSettingsOpen] = useState(false);
  // The next entry starts with no window open, whichever way this one ends.
  useEffect(() => () => ui.getState().clear(), []);
  useEffect(() => {
    if (me === '') {
      return undefined;
    }
    const stop = watchSkillBar(rest, me, (text) => toasts.getState().push(text));
    return () => keepSaves(stop());
  }, [rest, me, keepSaves]);
  useEffect(() => {
    if (me === '') {
      return undefined;
    }
    const stop = watchArenaBuild(rest, me, (text) => toasts.getState().push(text));
    return () => keepSaves(stop());
  }, [rest, me, keepSaves]);
  useEffect(() => (me === '' ? undefined : watchPvpProfile(rest, socket, me)), [rest, socket, me]);
  // The pieces as a character enters, for the inventory's Add; one that fails leaves the server to refuse a piece added twice.
  useEffect(() => (me === '' ? undefined : loadWardrobe(rest, () => undefined)), [rest, me]);
  useEffect(() => watchPlaySettings(rest, (text) => toasts.getState().push(text)), [rest]);
  // Any call a rate limit refused says so, whether its caller tells its own failures or keeps quiet.
  useEffect(
    () => rest.onLimited(() => toasts.getState().push(errorText(ERR.RATE_LIMITED))),
    [rest],
  );
  return (
    <>
      <Hud />
      <ConnectionNotice />
      <TopBars>
        <MatchBar socket={socket} />
        <EnemyFrames />
      </TopBars>
      <MatchCount />
      <Minimap />
      <QueueTag socket={socket} />
      <TeleportBar />
      <DeathOverlay />
      <Toasts />
      <div className="world-corner">
        <button type="button" className="btn btn-quiet" onClick={() => setSettingsOpen(true)}>
          Settings
        </button>
        <button type="button" className="btn btn-quiet" onClick={onLeave}>
          Back to characters
        </button>
        <PartyPanel />
        <TeamFrames />
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
          <CharacterWindow
            socket={socket}
            figureIn={figureIn}
            onClose={() => ui.getState().closeWindow('character')}
          />
        )}
        {skillsOpen && (
          <SkillWindow socket={socket} onClose={() => ui.getState().closeWindow('skills')} />
        )}
        {questsOpen && (
          <QuestLog socket={socket} onClose={() => ui.getState().closeWindow('quests')} />
        )}
        {mapOpen && (
          <MapWindow
            socket={socket}
            layoutOf={layoutOf}
            onClose={() => ui.getState().closeWindow('map')}
          />
        )}
        {arenaOpen && (
          <ArenaWindow
            socket={socket}
            board={board}
            onClose={() => ui.getState().closeWindow('arena')}
          />
        )}
        {npcOpen && <NpcWindow socket={socket} onClose={() => ui.getState().closeWindow('npc')} />}
        {lootOpen && (
          <LootWindow socket={socket} onClose={() => ui.getState().closeWindow('loot')} />
        )}
        {wardrobeOpen && (
          <Wardrobe
            socket={socket}
            rest={rest}
            figureIn={figureIn}
            onClose={() => ui.getState().closeWindow('wardrobe')}
          />
        )}
      </div>
      <MatchOffer socket={socket} />
      <MatchResult />
      {settingsOpen && (
        <Modal title="Settings" onClose={() => setSettingsOpen(false)}>
          <SettingsPanel rest={rest} onClose={() => setSettingsOpen(false)} />
        </Modal>
      )}
    </>
  );
}
