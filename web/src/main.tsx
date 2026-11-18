import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';

import { Root } from '@/app/Root';
import '@/styles/global.css';

const root = document.getElementById('ui');
if (!root) {
  throw new Error('index.html has no #ui element');
}
createRoot(root).render(
  <StrictMode>
    <Root />
  </StrictMode>,
);
