// The one holder of the session token (page 7, Session token; page 4,
// Trust model). Set from the login response, replaced only when a Welcome
// arrives, cleared on logout or a Kicked that ends the session. Memory
// only: a reload means logging in again. Nothing else keeps a copy, because
// a REST call with the token from before the last Hello works once within
// 60 seconds and then revokes the whole session.
let current: string | null = null;

export function getToken(): string | null {
  return current;
}

export function setToken(token: string): void {
  if (token === '') {
    throw new Error('an empty session token');
  }
  current = token;
}

export function clearToken(): void {
  current = null;
}
