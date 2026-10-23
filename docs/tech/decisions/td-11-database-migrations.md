# TD-11. Database migrations

- **Area:** Data
- **Decision:** TD-11. Database migrations
- **Default:** goose
- **Outcome:** goose, over golang-migrate. One file per migration, a transaction per migration, Go migrations when SQL isn't enough, and no dirty-state lock.
- **Status:** Decided

## Outcome

goose, embedded in the server binary and run on startup or with a migrate subcommand.
