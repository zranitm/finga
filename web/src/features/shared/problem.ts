// What a refused or unanswered request tells the player.
import { RequestError } from '@/net/socket';

/** The client's words for the server's refusal; a request with no answer, timed out or cut off by a dropped connection, may still land, so it says only that. */
export function problemOf(err: unknown): string {
  if (err instanceof RequestError && (err.code === 'timeout' || err.code === 'disconnected')) {
    return 'No answer yet. Try again.';
  }
  return err instanceof Error ? err.message : 'The server refused.';
}
