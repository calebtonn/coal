import '@testing-library/jest-dom/vitest';
import { cleanup } from '@testing-library/svelte';
import { afterEach, vi } from 'vitest';

afterEach(() => {
  cleanup();
  globalThis.localStorage?.clear();
  vi.unstubAllGlobals();
});

// jsdom has no matchMedia; the theme store and AppShell read it. Node-environment tests have no window.
if (typeof window !== 'undefined') window.matchMedia ??= (query: string) =>
  ({ matches: false, media: query, addEventListener() {}, removeEventListener() {} }) as unknown as MediaQueryList;
