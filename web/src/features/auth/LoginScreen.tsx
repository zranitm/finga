import { useState } from 'react';
import type { FormEvent } from 'react';

import { Button } from '@/components/Button';
import { useKeyboardCapture } from '@/features/shared/use-keyboard-capture';
import { RestError } from '@/net/rest';
import type { RestClient } from '@/net/rest';
import { connection } from '@/state/connection';

import { passwordProblem, usernameProblem } from './validation';

export interface LoginScreenProps {
  readonly rest: RestClient;
  readonly onLoggedIn: (username: string) => void;
}

const kickMessages: Record<string, string> = {
  logged_in_elsewhere: 'This account logged in somewhere else.',
  session_expired: 'Your session ended. Log in again.',
  unauthenticated: 'Your session ended. Log in again.',
  abuse: 'The server closed the connection.',
  outdated_protocol: 'A new version is available; reload the page.',
  server_restart: 'The server restarted.',
};

/** Sign up and log in (A1). Login and sign-up share one form with a mode switch. */
export function LoginScreen({ rest, onLoggedIn }: LoginScreenProps) {
  const [mode, setMode] = useState<'login' | 'signup'>('login');
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [problem, setProblem] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const capture = useKeyboardCapture();
  const kick = connection.use((s) => s.kick);
  const status = connection.use((s) => s.status);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    const local = usernameProblem(username) ?? passwordProblem(password);
    if (local) {
      setProblem(local);
      return;
    }
    setBusy(true);
    setProblem(null);
    try {
      if (mode === 'signup') {
        await rest.register(username, password);
      }
      await rest.login(username, password);
      onLoggedIn(username);
    } catch (err) {
      if (err instanceof RestError && err.code === 'login_rate_limited') {
        setProblem('Too many login attempts. Wait a minute, then try again.');
      } else if (err instanceof RestError) {
        setProblem(err.message);
      } else {
        setProblem('Something went wrong. Try again.');
      }
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="screen">
      <form
        className="form"
        onSubmit={(e) => void submit(e)}
        aria-label={mode === 'login' ? 'Log in' : 'Sign up'}
      >
        <h1>Finga</h1>
        {status === 'kicked' && kick && <p className="notice">{kickMessages[kick] ?? kick}</p>}
        <label className="field">
          Username
          <input
            name="username"
            autoComplete="username"
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            {...capture}
          />
        </label>
        <label className="field">
          Password
          <input
            name="password"
            type="password"
            autoComplete={mode === 'login' ? 'current-password' : 'new-password'}
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            {...capture}
          />
        </label>
        {problem && (
          <p className="error" role="alert">
            {problem}
          </p>
        )}
        <div className="row">
          <Button type="submit" disabled={busy}>
            {mode === 'login' ? 'Log in' : 'Create account'}
          </Button>
          <Button
            variant="quiet"
            onClick={() => {
              setMode(mode === 'login' ? 'signup' : 'login');
              setProblem(null);
            }}
          >
            {mode === 'login' ? 'New here? Sign up' : 'Have an account? Log in'}
          </Button>
        </div>
      </form>
    </div>
  );
}
