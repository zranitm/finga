// The page's last line of defence: an error React can't recover from shows a reload screen, never a blank page, and goes to the console.
import type { ErrorInfo } from 'react';
import { ErrorBoundary } from 'react-error-boundary';

import { Button } from '@/components/Button';

import { App } from './App';

function Crashed() {
  return (
    <div className="screen">
      <div className="panel" role="alert">
        <h2>Something went wrong</h2>
        <p>Reload the page to play again.</p>
        <Button onClick={() => window.location.reload()}>Reload</Button>
      </div>
    </div>
  );
}

function report(error: unknown, info: ErrorInfo) {
  console.error('finga: the page crashed', error, info.componentStack);
}

export function Root() {
  return (
    <ErrorBoundary FallbackComponent={Crashed} onError={report}>
      <App />
    </ErrorBoundary>
  );
}
