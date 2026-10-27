# Stage 2: Extend

Complete the game and grow the architecture to the target in [High-level design](../high-level-design.md). Starts once Stage 1's exit criteria are met.

## Product scope

Everything not in Stage 1. Feature details are in [Features](../../product/features/README.md).

- **First:** Thief and Magician, then the P1 features: F1 and F3 (currency and the marketplace), A3, B4, C5, D3, D4, E2, F2, F5.
- **Then:** the P2 features: A4, C6, C7, E4, F4, F6.
- **Gacha (G1):** only if OD-01 makes it a mechanic.

## Architecture steps

In order. Each step brings the system closer to the High-level design.

1. **Public demo:** nginx, Cloudflare, and Terraform on one VPS (TD-05, TD-12, TD-13).
2. **Event pipeline:** outbox relay and Kafka; the auditor becomes a consumer; trade-history and price-candles arrive with F5 (TD-04, TD-14).
3. **Protocol:** Protobuf with buf and breaking-change checks.
4. **Observability:** Prometheus and Grafana dashboards.
5. **Load and chaos:** the bot client, load tests to the TD-08 target, and chaos tests verified by the auditor.
6. **Scale out:** channels (B4), then more nodes per service: several gateways and world nodes, a registry of which node owns which map, NATS in place of the delivery streams, and economy and market inside the item service (HLD section 15). The split into services itself happened in Stage 1 (TD-25).

## Exit criteria

- The architecture matches the High-level design.
- A public demo link.
- Published load-test numbers at the TD-08 target.
- The auditor finds zero mismatches after chaos tests.

## Temporary choices

None planned. Any that come up follow the same rule as Stage 1: each names its permanent target and the seam that keeps the switch cheap.
