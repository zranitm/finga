# infra

How Finga runs, apart from the code that runs.

- `docker-compose.yml`: `make up` and `make down`. Postgres, the migrations
  as a one-shot job, and the game server, built from `../server` with
  `server/Dockerfile`. The host ports come from `infra/.env` (see
  `.env.example`); the game server's settings from `server/.env`, which
  Compose passes to the container.
- `obs/`: the observability stack, which `make up OBS=1` runs beside the
  game under the `obs` profile; plain `make up` leaves it out. Grafana, at
  http://localhost:3000, shows the Finga dashboard over Mimir (metrics),
  Loki (logs) and Tempo (traces). Alloy feeds all three: it scrapes each
  service's `/metrics`, reads the containers' logs, and relays the traces
  the services keep (REST requests, answered commands, pickups). A log
  line links to its trace, and a trace to its logs.

Later stages add their parts here: infrastructure as code for a hosted
deployment.
