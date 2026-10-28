# 15. Chat and party

- **Depends on:** 10, 11, 13, 14
- **Features:** E1, E3
- **Milestone:** M5. Play together
- **Order:** 15
- **Status:** Done
- **Step:** 15. Chat and party

> **Changed by step 25 (2026-10-28):** parties and chat run in the social service (`services/social`); it delivers PartyState, invites and party chat to each member's gateway on its stream, and the world reads a local copy of party membership fed by `WatchParties`.

**Goal:** map chat, and parties of up to 4 that share EXP and loot. This completes M5, Play together.

## Scope

- `internal/application/chat`: map and party channels, up to 120 characters, control characters stripped, rate-limited to 1 per second with a burst of 3.
- `internal/application/party`, one actor (TD-19):
  - Create, invite (expires after 30 seconds), accept or decline, leave, kick, and promote.
  - When the leader leaves, the longest-standing member leads. Offline members stay. The party disbands when empty. At most 4 members.
  - PartyState pushed to members.
- EXP sharing with the OD-04 default: members alive on the same map share it equally with a 10% bonus per extra member, and the remainder goes to the killer.
- The loot window from step 13 now covers the killer's party (OD-05 default).
- Party chat works across maps.
- Gateway handlers for the party and chat messages, calling the services with the connection's identity. Chat and party are application services with no repository (TD-19).

## Tests first

- the party lifecycle as a table: create, invite, accept, decline, leave, kick, promote, disband
- invites expire; a fifth member is rejected
- the leader leaves and the longest-standing member takes over
- EXP split rows for 1 to 4 members, including the remainder, dead members, and members on other maps
- party members can loot inside the window; others can't
- chat: control characters removed, the length limit, the rate limit; map chat reaches only the same map
- end-to-end rows for E1 and E3

## Done when

- [ ] Four test clients can party up, hunt, share EXP, and chat.

## Review focus

- Party state stays consistent when members disconnect and reconnect.
