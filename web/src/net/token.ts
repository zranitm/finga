// Memory only, and the only copy: a REST call with a token older than the last Hello revokes the session.
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
