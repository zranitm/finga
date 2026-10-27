# 2. Data model

## Conventions

- Postgres, one schema (`public`, TD-22). Table names carry no schema prefix.
- Each module owns its tables (see Table ownership). No module reads or writes another module's tables.
- IDs are UUIDv7, generated in Go.
- Coins are `bigint` whole units; no decimals.
- Timestamps are `timestamptz`.
- No foreign keys across modules: modules refer to each other by ID only, so a module's tables can later move to their own database.
- Migrations: goose, one ordered set per service with tables, `services/<service>/migrations/` (account, character, item), embedded through its `embed.go` and recorded in the service's own version table (`goose_account_version`, `goose_character_version`, `goose_item_version`), so each service's schema changes on its own. `cmd/migrate` applies every set, account first, and refuses a database migrated before the split, which has goose's default `goose_db_version` (changed 2026-10-27, step 21; Low-level design, page 10).
- Queries: sqlc generates typed code per module from `internal/<module>/queries`, under `internal/application/`, checked against its own service's migrations only, so a query cannot read another service's table.

## Table ownership

| Owner | Tables |
| --- | --- |
| `account` | `accounts`, `sessions` |
| `character` | `characters` |
| `item` | `items`, `item_events`, and, through `pkg/outbox` and `pkg/idempotency`, `outbox` and `idempotency_keys` |
| `economy`, through `pkg/ledger` (Stage 2) | `ledger_accounts`, `ledger_transfers`, `ledger_postings` |
| `market` (Stage 2) | `listings` |

`outbox` and `idempotency_keys` were shared tables until step 21; only the item module uses them, so the item service owns them. A later service that needs an outbox gets its own table. `citext` is created by both the account and the character service, and dropped by neither.

## account

```sql
CREATE TABLE accounts (
  id            uuid PRIMARY KEY,
  username      citext NOT NULL UNIQUE,
  password_hash text NOT NULL,                 -- argon2id, PHC string format
  settings      jsonb NOT NULL DEFAULT '{}',   -- control preset (B6)
  created_at    timestamptz NOT NULL
);

CREATE TABLE sessions (
  id          uuid PRIMARY KEY,
  account_id  uuid NOT NULL REFERENCES accounts(id),
  token_hash  bytea NOT NULL UNIQUE,           -- SHA-256 of the opaque token (TD-01)
  previous_token_hash bytea,                   -- the token retired by the last rotation (step 16)
  rotated_at  timestamptz,                     -- when; cleared once the previous token has been used
  created_at  timestamptz NOT NULL,
  expires_at  timestamptz NOT NULL,            -- sliding: 7 days idle, pushed at most every 10 minutes, never past 30 days from created_at
  revoked_at  timestamptz
);
CREATE INDEX sessions_by_previous_token
  ON sessions (previous_token_hash) WHERE previous_token_hash IS NOT NULL;
-- one active session per account (OD-08)
CREATE UNIQUE INDEX one_active_session
  ON sessions (account_id) WHERE revoked_at IS NULL;
```

## character and item

```sql
CREATE TABLE characters (
  id             uuid PRIMARY KEY,
  account_id     uuid NOT NULL,
  name           citext NOT NULL UNIQUE,
  class          text NOT NULL,                -- warrior | bowman | thief | magician
  level          int NOT NULL CHECK (level >= 1),
  exp            bigint NOT NULL CHECK (exp >= 0),  -- EXP within the current level
  strength       int NOT NULL,
  dexterity      int NOT NULL,
  intelligence   int NOT NULL,
  agility        int NOT NULL,
  vitality       int NOT NULL,
  unspent_points int NOT NULL DEFAULT 0,
  hp             int NOT NULL,
  mp             int NOT NULL,
  map_id         text NOT NULL,
  pos_x          real NOT NULL,
  pos_y          real NOT NULL,
  updated_at     timestamptz NOT NULL
);

CREATE TABLE items (
  id          uuid PRIMARY KEY,
  template_id text NOT NULL,                   -- from game data
  owner_id    uuid NOT NULL,                   -- character
  location    text NOT NULL,                   -- inventory | listed | equipped (Stage 2) | destroyed
  tab         text,                            -- equip | use | etc, only in the inventory
  slot        int,
  quantity    int NOT NULL CHECK (quantity >= 0),
  updated_at  timestamptz NOT NULL,
  CHECK ((location = 'inventory') = (tab IS NOT NULL AND slot IS NOT NULL)),
  CHECK ((location = 'destroyed') = (quantity = 0))
);
CREATE UNIQUE INDEX one_item_per_slot
  ON items (owner_id, tab, slot) WHERE location = 'inventory';

-- append-only history of every item (D5)
CREATE TABLE item_events (
  id             bigserial PRIMARY KEY,
  item_id        uuid NOT NULL REFERENCES items(id),
  kind           text NOT NULL,   -- created | stacked | split | listed | unlisted | sold | discarded
  quantity_delta int NOT NULL,    -- change to this item's quantity
  from_owner     uuid,
  to_owner       uuid,
  cause_type     text NOT NULL,   -- drop | market | player
  cause_id       uuid,            -- drop, listing, or request ID
  occurred_at    timestamptz NOT NULL
);
```

## economy (Stage 2)

These tables belong to `pkg/ledger`. Their `ledger_` prefix is configurable, so the package can sit beside other tables in any project.

```sql
CREATE TABLE ledger_accounts (
  id             text PRIMARY KEY,             -- wallet:<character id>, system:mint, ...
  balance        bigint NOT NULL DEFAULT 0,
  allow_negative boolean NOT NULL DEFAULT false,
  CHECK (allow_negative OR balance >= 0)
);

CREATE TABLE ledger_transfers (
  id         uuid PRIMARY KEY,
  kind       text NOT NULL,
  meta       jsonb NOT NULL,
  created_at timestamptz NOT NULL
);

CREATE TABLE ledger_postings (
  id          bigserial PRIMARY KEY,
  transfer_id uuid NOT NULL REFERENCES ledger_transfers(id),
  account_id  text NOT NULL REFERENCES ledger_accounts(id),
  amount      bigint NOT NULL CHECK (amount <> 0)
);
-- A deferred constraint trigger checks at commit that each transfer's postings sum to zero.
```

## market (Stage 2)

```sql
CREATE TABLE listings (
  id          uuid PRIMARY KEY,
  seller_id   uuid NOT NULL,
  item_id     uuid NOT NULL,
  template_id text NOT NULL,                   -- copied for search
  quantity    int NOT NULL CHECK (quantity > 0),
  price       bigint NOT NULL CHECK (price > 0),
  fee_bps     int NOT NULL,                    -- fee at listing time, in basis points (OD-10: 500)
  status      text NOT NULL,                   -- active | sold | cancelled | expired | reclaimed
  buyer_id    uuid,
  created_at  timestamptz NOT NULL,
  expires_at  timestamptz NOT NULL,
  closed_at   timestamptz
);
CREATE UNIQUE INDEX one_open_listing_per_item
  ON listings (item_id) WHERE status IN ('active', 'expired');
CREATE INDEX active_by_template_price
  ON listings (template_id, price) WHERE status = 'active';
CREATE INDEX active_by_expiry
  ON listings (expires_at) WHERE status = 'active';
```

Search by name or type: game data maps the text or type to template IDs in memory, then the query filters by `template_id`.

## platform

The tables `pkg/outbox` and `pkg/idempotency` write, created by the item service's migrations. Each package also holds the same DDL as `Schema`, for its own tests; a test in the item service checks the two agree.

```sql
-- pkg/outbox
CREATE TABLE outbox (
  id           bigserial PRIMARY KEY,
  event_id     uuid NOT NULL UNIQUE,
  topic        text NOT NULL,
  key          text NOT NULL,
  type         text NOT NULL,
  content_type text NOT NULL,                  -- application/json now, protobuf in Stage 2
  payload      bytea NOT NULL,
  occurred_at  timestamptz NOT NULL,
  published_at timestamptz                     -- set by the relay (Stage 2)
);
CREATE INDEX outbox_unpublished ON outbox (id) WHERE published_at IS NULL;

-- pkg/idempotency
CREATE TABLE idempotency_keys (
  owner        text NOT NULL,                  -- account ID
  scope        text NOT NULL,                  -- command type
  key          text NOT NULL,
  request_hash bytea NOT NULL,
  response     bytea NOT NULL,
  created_at   timestamptz NOT NULL,
  PRIMARY KEY (owner, scope, key)
);
```

Idempotency keys and revoked or expired sessions are deleted by an hourly cleanup job after 24 hours.

## Not in the database

- Positions, HP, and MP between checkpoints.
- Mobs and ground drops.
- Parties, invites, and chat (TD-19).

## Game data files

```txt
data/movement.yaml      movement constants (page 5), shared with the client
data/classes.yaml       class definitions, attack shapes, cooldowns, base stats, level-up preset, starter items
data/stats.yaml         derived stat coefficients and caps per class
data/exp_curve.yaml     EXP needed per level
data/mobs.yaml          mob stats, behavior, drop table
data/items.yaml         item templates
data/maps/*.tmj         Tiled maps, shared by client and server
```

Movement test vectors are test fixtures, so they live in `testdata/movement/`.

`gamedata` validates everything at startup: an unknown ID or a broken reference stops the server.
