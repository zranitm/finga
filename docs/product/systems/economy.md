# Economy

## Currency

- One in-game currency. No real money anywhere in the game.
- A balance never goes below zero.
- Currency is never created or lost by mistake, and every change has a traceable cause: a drop, a shop, a trade, or a fee.

## Sources and sinks

- **Sources (currency enters the game):** mob drops, and selling to NPC shops (P1).
- **Sinks (currency leaves the game):** buying from NPC shops (P1), and the marketplace fee on each sale.
- **Transfers (currency moves between players):** marketplace purchases, and direct trades (P2).
- Goal: over time, sinks roughly balance sources, so prices stay stable.

## Marketplace

- Opened from a marketplace NPC in towns (proposal).
- A seller lists an item at a price. The item leaves their inventory while it's listed.
- Buyers browse and search by name, type, and price.
- A purchase happens fully or not at all: the buyer pays, the seller receives the price minus the fee, and the buyer gets the item.
- Only one buyer can buy a given listing.
- Proceeds reach the seller's wallet even if the seller is offline.
- A seller can cancel a listing to get the item back. Unsold listings expire after a set time and the item returns.
- Model: OD-07 (default: fixed-price listings first, an order book later).
- Fee and expiry: OD-10 (default: 5% of the sale price, 7 days).

## NPC shops (P1)

- Fixed prices.
- An NPC always buys for less than it sells, so shops can't be used to make money.

## Later

- Direct trade (P2), price history (P1), leaderboards (P2).
