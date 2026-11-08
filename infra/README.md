# infra

How Finga runs, apart from the code that runs.

- `docker-compose.yml`: `make up` and `make down`. Postgres, the migrations
  as a one-shot job, the six services (gateway, account, character, item,
  world and social), each built from `../server` with `server/Dockerfile`,
  and the web client, built from `../web`. The host ports come from
  `infra/.env` (see `.env.example`); the services' settings from
  `server/.env`, which Compose passes to each of the six services (migrate
  takes only the database settings).
- `nginx/`: the web client's server. It serves the client and passes `/api`
  and `/ws` to the gateway, so the browser sees one origin, and holds the
  limits per client address.
- `obs/`: the observability stack, which `make up OBS=1` runs beside the
  game under the `obs` profile; plain `make up` leaves it out. Grafana, at
  http://localhost:3000, shows the Finga dashboard over Mimir (metrics),
  Loki (logs) and Tempo (traces). Alloy feeds all three: it scrapes each
  service's `/metrics`, reads the containers' logs, and relays the traces
  the services keep (REST requests, answered commands, pickups). A log
  line links to its trace, and a trace to its logs.

- `deploy/`: production on AWS, behind Cloudflare. The Compose overlay, the
  deploy and backup scripts the instance runs, and the runbook
  (`deploy/README.md`), which starts there.
- `terraform/`: the AWS and Cloudflare side. Generic modules (an EC2 host for
  a Compose app, ECR repositories, a private bucket, a GitHub OIDC role, a
  Cloudflare-proxied origin, Cloudflare Access) and `envs/prod`, which wires
  them for Finga.
