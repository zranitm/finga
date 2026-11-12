// A refused line comes back as a Response with no req; app/wiring shows it as a toast.
import { useEffect, useLayoutEffect, useRef, useState } from 'react';
import type { KeyboardEvent, ReactNode } from 'react';

import { itemName, starClass, starMarks } from '@/features/shared/items';
import { saveSettings } from '@/features/shared/settings';
import { article } from '@/features/shared/words';
import { limits } from '@/gamedata/limits';
import type { RestClient } from '@/net/rest';
import type { GameSocket } from '@/net/socket';
import { className } from '@/progression/classes';
import { npcById, objectiveLabel, questById, questName } from '@/progression/quests';
import { ChatChannel, LootKind } from '@/protocol/messages';
import { character } from '@/state/character';
import { chat } from '@/state/chat';
import { combatLog } from '@/state/combat-log';
import type { CombatLine, QuestLineKind } from '@/state/combat-log';
import { settings } from '@/state/settings';
import { toasts } from '@/state/toasts';
import { ui } from '@/state/ui';

export const MAX_CHAT_CHARS = limits.chat_line_max;
// Idle, the last lines sit below a standing character's feet; open, the log scrolls through all its store keeps.
const IDLE_LINES = 3;

export type ChatTab = 'general' | 'party' | 'combat';

// The Combat tab has no channel of its own, so what is typed there goes to the map.
const PLACEHOLDERS: Record<ChatTab, string> = {
  general: 'Enter to chat; /p for the party',
  party: 'Enter to chat with the party',
  combat: 'Enter to chat on the map; /p for the party',
};

const TABS: readonly { readonly id: ChatTab; readonly label: string }[] = [
  { id: 'general', label: 'General' },
  { id: 'party', label: 'Party' },
  { id: 'combat', label: 'Combat' },
];

export interface ChatBoxProps {
  readonly socket: Pick<GameSocket, 'send'>;
  readonly rest: Pick<RestClient, 'putSettings'>;
}

/** "/p hello" is a party line from any tab; otherwise the Party tab sends party lines and the others map lines. */
export function parseLine(raw: string, tab: ChatTab): { channel: ChatChannel; text: string } {
  const m = /^\/p(?:\s+|$)(.*)$/s.exec(raw);
  if (m) {
    return { channel: ChatChannel.PARTY, text: m[1]!.trim() };
  }
  return { channel: tab === 'party' ? ChatChannel.PARTY : ChatChannel.MAP, text: raw.trim() };
}

/** Idle, only the tab's last few lines; open while typing or pinned, the tabs, the log and the input. */
export function ChatBox({ socket, rest }: ChatBoxProps) {
  const open = ui.use((s) => s.chatOpen);
  const pinned = settings.use((s) => s.chatPinned);
  const [tab, setTab] = useState<ChatTab>('general');
  const [draft, setDraft] = useState('');
  const input = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (open) {
      input.current?.focus();
    }
  }, [open]);

  const { channel, text } = parseLine(draft, tab);
  const length = [...text].length; // characters, as the server counts them, so an emoji is one
  const over = length > MAX_CHAT_CHARS;

  const onKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.nativeEvent.isComposing || e.keyCode === 229) {
      return; // the IME's: Enter commits the composition, Escape cancels it; Safari reports these as 229
    }
    if (e.key === 'Escape') {
      e.preventDefault();
      input.current?.blur();
      return;
    }
    if (e.key !== 'Enter') {
      return;
    }
    e.preventDefault();
    if (text === '' || over) {
      return;
    }
    socket.send('chatSend', { channel, text });
    setDraft('');
    input.current?.blur();
  };

  const togglePin = async () => {
    settings.getState().setChatPinned(!pinned);
    const problem = await saveSettings(rest);
    if (problem) {
      toasts.getState().push(problem);
    }
  };

  if (!open && !pinned) {
    return (
      <div
        className="chat chat-idle"
        aria-label="Chat"
        title="Enter or click to chat"
        onClick={() => ui.getState().openChat()}
      >
        <Lines tab={tab} idle />
      </div>
    );
  }

  const label = TABS.find((t) => t.id === tab)!.label;
  return (
    <div className="chat" aria-label="Chat">
      <div className="chat-head">
        <div className="chat-tabs" role="tablist" aria-label="Chat tabs">
          {TABS.map((t) => (
            <button
              key={t.id}
              type="button"
              role="tab"
              aria-selected={t.id === tab}
              className="chat-tab"
              onMouseDown={(e) => e.preventDefault()} // keeps the focus, and a draft being typed, in the input
              onClick={() => setTab(t.id)}
            >
              {t.label}
            </button>
          ))}
        </div>
        <button
          type="button"
          className="chat-tab chat-pin"
          aria-pressed={pinned}
          title="Keep the chat open while you play"
          onMouseDown={(e) => e.preventDefault()}
          onClick={() => void togglePin()}
        >
          Pin
        </button>
      </div>
      <div role="tabpanel" aria-label={label}>
        <Lines tab={tab} idle={false} />
      </div>
      <div className="chat-input-row">
        <input
          ref={input}
          aria-label="Chat message"
          placeholder={PLACEHOLDERS[tab]}
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={onKeyDown}
          onFocus={() => ui.getState().openChat()}
          onBlur={() => ui.getState().closeChat()}
        />
        <span className={`chat-count${over ? ' chat-over' : ''}`} aria-label="Characters">
          {length}/{MAX_CHAT_CHARS}
        </span>
      </div>
    </div>
  );
}

// Each tab's log is its own list, so a tab chosen starts at its newest line.
function Lines({ tab, idle }: { readonly tab: ChatTab; readonly idle: boolean }) {
  return (
    <div className="chat-log">
      {tab === 'combat' ? (
        <CombatLines idle={idle} />
      ) : (
        <ChatLines key={tab} partyOnly={tab === 'party'} idle={idle} />
      )}
    </div>
  );
}

// The Party tab shows party lines only; a notice shows in General.
function ChatLines({ partyOnly, idle }: { readonly partyOnly: boolean; readonly idle: boolean }) {
  const lines = chat.use((s) => s.lines);
  const shown = partyOnly
    ? lines.filter((l) => l.kind === 'said' && l.channel === ChatChannel.PARTY)
    : lines;
  return (
    <LogList
      idle={idle}
      lines={shown.map((l) => {
        if (l.kind === 'notice') {
          return {
            key: `${l.at}-notice-${l.text}`,
            className: 'chat-notice',
            body: (
              <>
                <span className="chat-tag">[Notice]</span> {l.text}
              </>
            ),
          };
        }
        const party = l.channel === ChatChannel.PARTY;
        return {
          key: `${l.at}-${l.sender}-${l.text}`,
          className: party ? 'chat-party' : 'chat-map',
          body: (
            <>
              <span className="chat-tag">[{party ? 'Party' : 'Map'}]</span>{' '}
              <strong>{l.senderName}:</strong> {l.text}
            </>
          ),
        };
      })}
    />
  );
}

function CombatLines({ idle }: { readonly idle: boolean }) {
  const lines = combatLog.use((s) => s.lines);
  const me = character.use((s) => s.character?.id);
  return (
    <LogList
      idle={idle}
      lines={lines.map((l) => ({
        key: l.id,
        className: 'chat-combat',
        body: <CombatText line={l} me={me} />,
      }))}
    />
  );
}

interface LogLine {
  readonly key: string | number;
  readonly className: string;
  readonly body: ReactNode;
}

/** Idle, the last few lines from the bottom up, the CSS hiding any that doesn't fit whole; open, every line, scrolled to the newest unless the player scrolled up to read. */
function LogList({ lines, idle }: { readonly lines: readonly LogLine[]; readonly idle: boolean }) {
  const list = useRef<HTMLUListElement>(null);
  const following = useRef(true);
  const newest = lines.at(-1)?.key;
  useLayoutEffect(() => {
    const el = list.current;
    if (el && following.current) {
      el.scrollTop = el.scrollHeight;
    }
  }, [newest]);
  if (idle) {
    return (
      <ul className="chat-lines" aria-live="polite">
        {lines.slice(-IDLE_LINES).map((l, i) => (
          <li key={l.key} className={`chat-line ${l.className}`} style={{ order: -i }}>
            {l.body}
          </li>
        ))}
      </ul>
    );
  }
  return (
    <ul
      ref={list}
      className="chat-lines"
      aria-live="polite"
      onScroll={(e) => {
        const el = e.currentTarget;
        following.current = el.scrollHeight - el.scrollTop - el.clientHeight < 2;
      }}
    >
      {lines.map((l) => (
        <li key={l.key} className={`chat-line ${l.className}`}>
          {l.body}
        </li>
      ))}
    </ul>
  );
}

function CombatText({ line, me }: { readonly line: CombatLine; readonly me: string | undefined }) {
  switch (line.kind) {
    case 'exp':
      return line.amount < 0 ? (
        <>You lost {-line.amount} EXP.</>
      ) : (
        <>You gained {line.amount} EXP.</>
      );
    case 'level':
      return <>You reached level {line.level}!</>;
    case 'promoted': {
      const name = className(line.cls);
      return (
        <>
          You became {article(name)} {name}!
        </>
      );
    }
    case 'loot': {
      const item = <Item template={line.template} quantity={line.quantity} />;
      switch (line.loot) {
        case LootKind.COLLECTED:
          return (
            <>
              {line.ownerId === me ? 'You' : line.ownerName} got {item}.
            </>
          );
        case LootKind.WAITING:
          return <>Inventory full: {item} waits on the ground.</>;
        case LootKind.LOST:
          return <>{item} disappeared uncollected.</>;
      }
      return null;
    }
    case 'drank':
      return (
        <>
          Drank {article(itemName(line.template))} <Item template={line.template} quantity={1} />.
        </>
      );
    case 'evolved':
      return (
        <>
          {line.from} evolved into {line.to}!
        </>
      );
    case 'quest':
      return <QuestText change={line.change} quest={line.quest} progress={line.progress} />;
  }
}

function QuestText({
  change,
  quest,
  progress,
}: {
  readonly change: QuestLineKind;
  readonly quest: string;
  readonly progress: readonly number[];
}) {
  const name = <span className="chat-quest">{questName(quest)}</span>;
  switch (change) {
    case 'accepted':
      return <>Quest taken: {name}.</>;
    case 'progressed': {
      // Kills and talks: the counts the server keeps; items and levels show in the quest log.
      const counted = (questById(quest)?.objectives ?? []).flatMap((o, k) =>
        o.kind === 'kill' || o.kind === 'talk'
          ? [`${objectiveLabel(o)} ${Math.min(progress[k] ?? 0, o.count)}/${o.count}`]
          : [],
      );
      return (
        <>
          {name}: {counted.join(', ')}
        </>
      );
    }
    case 'ready': {
      const to = npcById(questById(quest)?.turn_in ?? '')?.name;
      return (
        <>
          {name} is ready to hand in{to ? ` to ${to}` : ''}.
        </>
      );
    }
    case 'finished': {
      // EXP has its own line; the items a quest gives would otherwise go unsaid.
      const items = questById(quest)?.rewards.items ?? [];
      return (
        <>
          Quest finished: {name}!
          {items.map((i, k) => (
            <span key={i.item}>
              {k === 0 ? ' You got ' : ', '}
              <Item template={i.item} quantity={i.quantity} />
            </span>
          ))}
          {items.length > 0 && '.'}
        </>
      );
    }
    case 'abandoned':
      return <>Quest abandoned: {name}.</>;
  }
}

/** "Iron Sword ★★" in its star colour, then "×3" for more than one. */
function Item({ template, quantity }: { readonly template: string; readonly quantity: number }) {
  return (
    <>
      <span className={`chat-item ${starClass(template)}`}>
        {itemName(template)} {starMarks(template)}
      </span>
      {quantity > 1 && ` ×${quantity}`}
    </>
  );
}
