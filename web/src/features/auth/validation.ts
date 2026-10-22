// The rules from page 4, checked before a request leaves; the server
// checks them again and its message wins when they disagree.
export const USERNAME_RULE = 'Username: 3 to 16 letters, digits or underscores.';
export const PASSWORD_RULE = 'Password: 8 to 128 characters.';

export function usernameProblem(username: string): string | null {
  return /^[A-Za-z0-9_]{3,16}$/.test(username) ? null : USERNAME_RULE;
}

export function passwordProblem(password: string): string | null {
  return password.length >= 8 && password.length <= 128 ? null : PASSWORD_RULE;
}
