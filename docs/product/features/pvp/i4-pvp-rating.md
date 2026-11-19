# I4. PvP rating and ranks

- **Feature:** I4. PvP rating and ranks
- **Group:** I. PvP
- **Priority:** P1
- **Status:** Agreed

**Story:** As a player, my 1v1 and 3v3 results move a rating for each, shown as a rank. A leaderboard shows the best in each.

## Acceptance

- **Two ratings per character:** one for 1v1 and one for 3v3. Being good alone and in a team are different things. 15v15 has none.
- **The rating:**
  - Starts at 1,000.
  - A win raises it and a loss lowers it, by more when the result was a surprise: beating a stronger side moves it more.
  - It moves quickly over the first matches, while the game is still learning the player, and settles as they play.
  - A draw moves it a little toward the other side's.
  - In 3v3 each player's rating moves by their team's result against the other team.
- **Bots count:** a match against bots moves the rating as one against players of the bots' strength. The bots met get stronger as the rating rises.
- **Ranks,** by rating:

  | Rank | Rating |
  | --- | --- |
  | Bronze | below 1,200 |
  | Silver | 1,200 |
  | Gold | 1,400 |
  | Platinum | 1,600 |
  | Diamond | 1,800 |
  | Master | 2,000 |

- **Placement:** for the first five matches of a mode, the rank shows as "Placing (3 of 5)". After them the rank shows.
- **The Arena window (V):**
  - **Play:** a card for each mode with the player's rating, rank, wins and losses, and the queue button. 15v15 shows wins and losses only.
  - **Leaderboard:** the top 100 of 1v1 or 3v3 by rating, with name, class, rank, rating and wins and losses, and the player's own place under it. Only players past placement are listed.
- **After a match** the result screen shows the rating before and after, and a new rank when it changes.
- **Nothing else changes:** a rating gives no items, EXP, currency or stats.
- **Rank outfits** (D9): the first time a character reaches Silver or a higher rank, in either mode, the account gets that rank's outfit piece. It's a look only.

## Not in this feature

- Seasons and season resets.
- Rewards for ranks, beyond the rank outfits.
- Guild ratings, which come with guild war.

## Rules

I1 for how matches work.
