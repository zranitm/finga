# 34. Social in layers

- **Depends on:** 33
- **Features:** none (structure only)
- **Milestone:** Layers
- **Order:** 34
- **Status:** Done
- **Step:** 34. Social in layers

**Goal:** the social service in the layers of page 11, with its behaviour unchanged.

**Layers in this step:** all of the social service's.

## Scope

- `internal/party` and `internal/chat` become `model` (`party.go`: Member, Party, Invite, the party errors; `chat.go`: Channel, Sender, Message, the line limits and `CleanLine`, the chat errors) and `application` (`party.go`: `PartyService` and its `Notifier` port, which the handler's streams implement; `chat.go`: `ChatService` and its `Parties` port). The names the two packages shared take their module's name (`PartyService`, `ChatService`, `ChannelMap`, `ErrChatNoParty`; decision 263). No repository: parties stay in memory (TD-19).
- Tests move with the code they test: rule tests with fakes to `application`, Postgres tests to `repository`, the error table's tests stay in `handler`.
- Page 10's tree and the README's architecture section show the social service's layers.

## Tests first

- none new: the step moves code, and the tests it moves must pass unchanged in what they assert; the layer rules of step 30 hold for the service

## Done when

- [x] `make up`, `make smoke`, `make test`, `make lint`, `make cover` green; `make test-service SERVICE=social` green; CI green.

## Review focus

- Imports point inward: nothing in `model` or `application` knows gRPC, Postgres or the configuration.
- No behaviour change hidden in the move.
