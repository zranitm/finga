# infra

How Finga runs, apart from the code that runs.

- `docker-compose.yml`: `make up` and `make down`. Postgres, the migrations
  as a one-shot job, the game (`finga`: every module in one process), each
  built from `../server` with a stage of `server/Dockerfile`, and the web
  client, built from `../web`. The host ports come from `infra/.env` (see
  `.env.example`); the game's settings from `server/.env`, which Compose
  passes to it (migrate takes only the database settings). Under the `tools`
  profile, which `up` leaves out, `testers` is `make testers`: a one-off job
  on the stack's own network that keeps the test accounts the root README
  lists. It writes through the game's private admin API, which only this
  network reaches (`finga:9200`); production keeps it off, and `make
  prod-check` (in CI) checks that.
- `nginx/`: the web client's server. It serves the client and passes `/api`
  and `/ws` to the game, so the browser sees one origin, and holds the
  limits per client address.
- `obs/`: the observability stack. Locally it is its own Compose project
  (`finga-obs`, through `obs/standalone.yml`), so Docker Desktop shows it as
  one group that stops and starts with one button. `make up OBS=1` runs it
  after the game; plain `make up` stops it. Alloy joins the game's network
  (`finga_default`) and reads only the game project's logs, so the game must
  be up before the stack starts. In
  production it stays in the game's project under the `obs` profile
  (`deploy/obs.prod.yml`), so a deploy that stops the game leaves it
  collecting the last logs. Grafana, at
  http://localhost:3000, shows the Finga dashboard over Mimir (metrics),
  Loki (logs) and Tempo (traces). Alloy feeds all three: it scrapes the
  game's `/metrics`, whose shared metrics carry a `module` label, reads the
  containers' logs (labelled by `level` and `module`), and relays the traces
  the game keeps (REST requests, answered commands, pickups). A log line
  links to its trace, and a trace to its logs.

- `deploy/`: production on AWS, behind Cloudflare. The Compose overlay, the
  deploy and backup scripts the instance runs, and the runbook
  (`deploy/README.md`), which starts there.
- `terraform/`: the AWS and Cloudflare side. Generic modules (an EC2 host for
  a Compose app, ECR repositories, a private bucket, a GitHub OIDC role, a
  Cloudflare-proxied origin, Cloudflare Access) and `envs/prod`, which wires
  them for Finga.

A second stack beside `finga`, say for a branch, takes its own project name
and host ports. Everything Compose creates for it, the network and volumes
too, is named after its project, so it never reaches the first:

```sh
COMPOSE_PROJECT_NAME=trial FINGA_WEB_PORT=4174 FINGA_PORT=8081 \
  FINGA_OPS_PORT=9110 FINGA_POSTGRES_PORT=5433 make up
```

With `OBS=1` and `FINGA_GRAFANA_PORT=3001`, its observability stack is
`trial-obs`, on `trial_default`, reading only `trial`'s logs. `make down` with
the same `COMPOSE_PROJECT_NAME` removes that stack alone.
