# infra

How Finga runs, apart from the code that runs.

- `docker-compose.yml`: `make up` and `make down`. Postgres, the migrations
  as a one-shot job, and the game server, built from `../server` with
  `server/Dockerfile`. The host ports come from `infra/.env` (see
  `.env.example`); the game server's settings from `server/.env`, which
  Compose passes to the container.

Later stages add their parts here: infrastructure as code for a hosted
deployment, and the Grafana dashboards and Prometheus configuration for the
metrics the game server exports at `/metrics`.
