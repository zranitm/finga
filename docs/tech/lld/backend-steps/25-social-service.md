# 25. Social service

- **Depends on:** 22
- **Features:** E1, E3
- **Milestone:** Services
- **Order:** 25
- **Status:** Ready for review
- **Step:** 25. Social service

**Goal:** parties and chat run as their own service. Social delivers party state, invites and party chat through its own stream, and the world reads a local copy of party membership, so the tick never waits on another service.

**Layers in this step:** the party and chat modules (moved), the social handler and binary; the game server's adapters and the world's party copy.

## Scope

**Social service**

- `internal/application/party` and `chat` move to `services/social/internal/party` and `chat` as they are.
- `services/social/internal/handler`: `SocialService` with its error table; presence (which gateway each online character is on, from `Online` and `Offline`); `Deliver` over `pkg/hub`, as the party module's notifier (PartyState to each member, PartyInvited to the invitee) and for party chat lines; `WatchParties` (Subscribed, every party's membership, then each change); `UpdateMember`.
- `services/social/cmd/social` and `app`.

**Game server**

- The gateway calls social for party commands, `Online` and `Offline`, `PartyOf` for Welcome, and `Chat`; a map line goes on to the map instance. It follows `social.Deliver` and turns each event into PartyState, PartyInvited or ChatMessage.
- A new boot ID on Subscribed means social restarted and the parties are gone: every player gets PartyState with no party, and is registered again with `Online`.
- The world's `Parties` becomes a party copy (an adapter in the composition root until step 27): a mutex-guarded table fed by `WatchParties`, cleared when the stream breaks, read by the loot window and EXP sharing; member updates go to a bounded queue and a worker calls `UpdateMember`, so the tick never waits (a full queue drops the update; the next one corrects it).
- `errmap` maps the party and chat reasons to the same codes. Compose and the end-to-end stack run social.

## Tests first

- handler over `bufconn`: party commands map their fields and reasons (table); the guard over both modules' errors; a member registered by `Online` gets PartyState and PartyInvited on their gateway's stream and nothing reaches another gateway; a party line reaches every member and a map line comes back to the caller; `WatchParties` sends the table on subscribe, then each change
- party copy: memberships applied, `SameParty` and `Members` answer from it, a broken stream clears it; a full update queue drops without blocking
- game server: a new social boot ID sends PartyState with no party to every player and registers them again
- end-to-end: E1 and E3 rows unchanged; new row: social restarts while two players are in a party, both sockets stay up and both see the party gone

## Done when

- [ ] `make up`, `make smoke`, `make test`, `make lint`, `make cover` green.

## Review focus

- The tick reads only the copy; nothing in a tick calls social.
- Party chat is delivered by social, not by the gateway, so it works when members sit on different gateways.
