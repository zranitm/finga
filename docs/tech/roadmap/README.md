# Roadmap

The game is built in two stages. Stage 1 proves the idea works with a minimal setup; Stage 2 extends the game and grows the architecture to the target in [High-level design](../high-level-design.md).

## How the docs split

- **Durable, stage-independent:** the Product docs, [Features](../../product/features/README.md), the High-level design (the target architecture), [Product decisions](../../product/decisions/README.md), and [Tech decisions](../decisions/README.md).
- **Stage pages:** only what is true for that stage. Scope as feature IDs, temporary choices, milestones, and exit criteria.

## Rules for stage pages

1. A stage page never holds the only copy of a permanent decision. Permanent choices go into Product decisions or Tech decisions, and the stage page links to them.
2. Temporary choices, true only during that stage, go in the stage page's "Temporary choices" table. Each one names the permanent target it gives way to, and the seam that keeps the switch cheap.
3. Scope is listed by feature ID. Feature details stay in Features.
4. When a stage ends: promote anything that turned out to be permanent into the decisions databases, then the stage page can be deleted.

- [Stage 1: Proof of concept](./stage-1-proof-of-concept.md)
- [Stage 2: Extend](./stage-2-extend.md)
