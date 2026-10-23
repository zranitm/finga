# infra

How Finga runs, apart from the code that runs.

- `docker-compose.yml`: `make up` and `make down`. Postgres, the migrations
  as a one-shot job, and the game server, built from `../server` with
  `server/Dockerfile`. Overrides go in `server/.env`.

Later stages add their parts here: infrastructure as code for a hosted
deployment, and the Grafana dashboards and Prometheus configuration for the
metrics the game server exports at `/metrics`.
