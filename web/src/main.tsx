import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';

import { App } from '@/app/App';

const root = document.getElementById('ui');
if (!root) {
  throw new Error('index.html has no #ui element');
}
createRoot(root).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
