# 2. Data model

## Conventions

- Postgres, one schema (`public`, TD-22). Table names carry no schema prefix.
- Each module owns its tables (see Table ownership). No module reads or writes another module's tables.
- IDs are UUIDv7, generated in Go by `pkg/uuidv7`; the client's request IDs and idempotency keys are UUIDv7 too (`web/src/lib/id.ts`).
- Coins are `bigint` whole units; no decimals.
- Timestamps are `timestamptz`.
- Every table has `created_at` and `updated_at`, kept by the database (see Timestamps): no query sets either.
- No foreign keys across modules: modules refer to each other by ID only, so a module's tables can later move to their own database.
- Migrations: goose, one ordered set per service with tables, `services/<service>/migrations/` (account, character, item), embedded through its `embed.go` and recorded in the service's own version table (`goose_account_version`, `goose_character_version`, `goose_item_version`), so each service's schema changes on its own. `cmd/migrate` applies every set, account first, and refuses a database migrated before the split, which has goose's default `goose_db_version` (changed 2026-10-27, step 21; Low-level design, page 10).
- Queries: sqlc generates typed code per module from `internal/<module>/queries`, under `internal/application/`, checked against its own service's migrations only, so a query cannot read another service's table.

## Table ownership

| Owner | Tables |
| --- | --- |
| `account` | `accounts`, `sessions` |
| `character` | `characters` |
| `item` | `items`, `item_events`, `starter_grants`, and, through `pkg/outbox` and `pkg/idempotency`, `outbox` and `idempotency_keys` |
| `economy`, through `pkg/ledger` (Stage 2) | `ledger_accounts`, `ledger_transfers`, `ledger_postings` |
| `market` (Stage 2) | `listings` |

`outbox` and `idempotency_keys` were shared tables until step 21; only the item module uses them, so the item service owns them. A later service that needs an outbox gets its own table. `citext` is created by both the account and the character service, and dropped by neither.

## Timestamps

Every table, append-only ones included, has the same two columns, which the database keeps (since 2026-11-05):

```sql
  created_at  timestamptz NOT NULL DEFAULT now(),
  updated_at  timestamptz NOT NULL DEFAULT now()
```

An insert takes both from their defaults, and each table has a trigger that sets `updated_at` on every update, whatever the statement wrote:

```sql
CREATE FUNCTION set_updated_at() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  NEW.updated_at := now();
  RETURN NEW;
END
$$;
CREATE TRIGGER set_updated_at BEFORE UPDATE ON accounts FOR EACH ROW EXECUTE FUNCTION set_updated_at();
```

- No query writes either column. A repository that needs one back reads it with `RETURNING` (a new account's `created_at`, a new character's `updated_at`).
- `now()` is the transaction's start, so every row a transaction writes gets the same time, and the time is the database's clock, not the service's.
- A time the game reasons about is a column of its own, set by the service: `occurred_at` (when an event happened, which consumers read), `expires_at`, `rotated_at`, `revoked_at`, `published_at`. The one rule that reads a row stamp is a session's 30-day cap, counted from its `created_at`.
- Every service with tables creates `set_updated_at()` with `CREATE OR REPLACE`, and none drops it, as with `citext`.
- Rows from before the columns existed were backfilled with the best time known: an account's `updated_at` is its `created_at`; a session's is the latest of its `created_at`, `rotated_at` and `revoked_at`; a character's `created_at` is its last save; an item's `created_at` is its first event (else its last change); item and outbox events take their `occurred_at` (an outbox row's `updated_at` its `published_at`, if later); `starter_grants.granted_at` became `created_at`.

The listings below show the two columns on every table.

## account

```sql
CREATE TABLE accounts (
  id            uuid PRIMARY KEY,
  username      citext NOT NULL UNIQUE,
  password_hash text NOT NULL,                 -- argon2id, PHC string format
  settings      jsonb NOT NULL DEFAULT '{}',   -- control preset (B6)
  created_at    timestamptz NOT NULL DEFAULT now(),
  updated_at    timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE sessions (
  id          uuid PRIMARY KEY,
  account_id  uuid NOT NULL REFERENCES accounts(id),
  token_hash  bytea NOT NULL UNIQUE,           -- SHA-256 of the opaque token (TD-01)
  previous_token_hash bytea,                   -- the token retired by the last rotation (step 16)
  rotated_at  timestamptz,                     -- when; cleared once the previous token has been used
  expires_at  timestamptz NOT NULL,            -- sliding: 7 days idle, pushed at most every 10 minutes, never past 30 days from created_at
  revoked_at  timestamptz,
  created_at  timestamptz NOT NULL DEFAULT now(),  -- the login
  updated_at  timestamptz NOT NULL DEFAULT now()
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
  created_at     timestamptz NOT NULL DEFAULT now(),
  updated_at     timestamptz NOT NULL DEFAULT now()  -- the last save; an account's list is in this order
);

CREATE TABLE items (
  id          uuid PRIMARY KEY,
  template_id text NOT NULL,                   -- from game data
  owner_id    uuid NOT NULL,                   -- character
  location    text NOT NULL,                   -- inventory | listed | equipped (Stage 2) | destroyed
  tab         text,                            -- equip | use | etc, only in the inventory
  slot        int,
  quantity    int NOT NULL CHECK (quantity >= 0),
  created_at  timestamptz NOT NULL DEFAULT now(),
  updated_at  timestamptz NOT NULL DEFAULT now(),
  CHECK ((location = 'inventory') = (tab IS NOT NULL AND slot IS NOT NULL)),
  CHECK ((location = 'destroyed') = (quantity = 0))
);
CREATE UNIQUE INDEX one_item_per_slot
  ON items (owner_id, tab, slot) WHERE location = 'inventory';

-- append-only history of every item (D5)
CREATE TABLE item_events (
  id             bigserial PRIMARY KEY,
  item_id        uuid NOT NULL REFERENCES items(id),
  kind           text NOT NULL,   -- created | stacked | split | moved | listed | unlisted | sold | discarded
  quantity_delta int NOT NULL,    -- change to this item's quantity
  from_owner     uuid,
  to_owner       uuid,
  cause_type     text NOT NULL,   -- drop | market | player | starter
  cause_id       uuid,            -- drop, listing, or request ID
  occurred_at    timestamptz NOT NULL,
  created_at     timestamptz NOT NULL DEFAULT now(),
  updated_at     timestamptz NOT NULL DEFAULT now()
);

-- one row per character given its starter kit, so a kit is granted once whatever happens to the
-- idempotency keys; GrantStarter runs on every join
CREATE TABLE starter_grants (
  character_id uuid PRIMARY KEY,
  created_at   timestamptz NOT NULL DEFAULT now(),  -- the grant (granted_at until 2026-11-05)
  updated_at   timestamptz NOT NULL DEFAULT now()
);
```

## economy (Stage 2)

These tables belong to `pkg/ledger`. Their `ledger_` prefix is configurable, so the package can sit beside other tables in any project.

```sql
CREATE TABLE ledger_accounts (
  id             text PRIMARY KEY,             -- wallet:<character id>, system:mint, ...
  balance        bigint NOT NULL DEFAULT 0,
  allow_negative boolean NOT NULL DEFAULT false,
  created_at     timestamptz NOT NULL DEFAULT now(),
  updated_at     timestamptz NOT NULL DEFAULT now(),
  CHECK (allow_negative OR balance >= 0)
);

CREATE TABLE ledger_transfers (
  id         uuid PRIMARY KEY,
  kind       text NOT NULL,
  meta       jsonb NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE ledger_postings (
  id          bigserial PRIMARY KEY,
  transfer_id uuid NOT NULL REFERENCES ledger_transfers(id),
  account_id  text NOT NULL REFERENCES ledger_accounts(id),
  amount      bigint NOT NULL CHECK (amount <> 0),
  created_at  timestamptz NOT NULL DEFAULT now(),
  updated_at  timestamptz NOT NULL DEFAULT now()
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
  expires_at  timestamptz NOT NULL,
  closed_at   timestamptz,
  created_at  timestamptz NOT NULL DEFAULT now(),
  updated_at  timestamptz NOT NULL DEFAULT now()
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

The tables `pkg/outbox` and `pkg/idempotency` write, created by the item service's migrations. Each package also holds the same DDL as `Schema`, for its own tests; a test in the item service checks the two agree, column defaults included. The update trigger is the service's, so `Schema` leaves it out.

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
  occurred_at  timestamptz NOT NULL,           -- the event's time, which consumers read
  published_at timestamptz,                    -- set by the relay (Stage 2)
  created_at   timestamptz NOT NULL DEFAULT now(),
  updated_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX outbox_unpublished ON outbox (id) WHERE published_at IS NULL;

-- pkg/idempotency
CREATE TABLE idempotency_keys (
  owner        text NOT NULL,                  -- account ID
  scope        text NOT NULL,                  -- command type
  key          text NOT NULL,
  request_hash bytea NOT NULL,
  response     bytea NOT NULL,
  created_at   timestamptz NOT NULL DEFAULT now(),
  updated_at   timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (owner, scope, key)
);
```

Nothing deletes idempotency keys or ended sessions yet, so both tables only grow. A cleanup of old keys is safe: no rule depends on a key outliving the retries it serves (starter kits are recorded in `starter_grants`).

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
