import { svelte } from '@sveltejs/vite-plugin-svelte';
import tailwindcss from '@tailwindcss/vite';
import { themeInitPlugin } from '@davenicoll/web-framework/vite';
import { writeFileSync } from 'node:fs';
import type { Plugin } from 'vite';
import { defineConfig } from 'vitest/config';

const daemon = 'http://127.0.0.1:4242';

// emptyOutDir clears dist/, including the tracked dist/.gitkeep that go:embed needs. Put it back.
const keepGitkeep = (): Plugin => ({
  name: 'coal-keep-gitkeep',
  apply: 'build',
  closeBundle: () => writeFileSync(new URL('./dist/.gitkeep', import.meta.url), ''),
});

export default defineConfig({
  base: '/',
  plugins: [tailwindcss(), svelte(), themeInitPlugin(), keepGitkeep()],
  build: { outDir: 'dist', emptyOutDir: true },
  server: { host: '127.0.0.1', proxy: { '/api': daemon, '/health': daemon } },
  resolve: {
    // The framework is a tree of symlinks into a checkout with no node_modules; resolve from here.
    preserveSymlinks: true,
    ...(process.env.VITEST ? { conditions: ['browser'] } : {}),
  },
  test: { environment: 'jsdom', include: ['src/**/*.test.ts'], setupFiles: ['./src/test/setup.ts'], css: false },
});
