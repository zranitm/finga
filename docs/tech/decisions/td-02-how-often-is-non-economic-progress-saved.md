# TD-02. How often is non-economic progress saved?

- **Area:** Data
- **Decision:** TD-02. How often is non-economic progress saved?
- **Default:** Every 30 seconds, plus on map change and logout
- **Status:** Open

## Scope

EXP, position, and HP. Economic state (items, currency) is always saved synchronously and isn't covered here.

## Trade-off

More frequent saves mean less rollback after a crash, but more database writes.

## Affects

HLD sections 2, 6, and 9.
