# COAL web UI

The page served by `coal serve` at `http://127.0.0.1:4242`. Svelte 5 and Vite, built on
`@davenicoll/web-framework`.

The framework is not published anywhere (#26). `package.json` points at the sibling checkout with
`file:../../web-framework`, so clone it next to this repo (`../web-framework` from the repo root)
before installing. Without it the UI does not build; the Go binary still builds, and `coal serve`
answers `/` with a page saying the UI is not built. Bun installs the framework as a tree of
symlinks, which is why `vite.config.ts` and `tsconfig.json` set `preserveSymlinks`.

```sh
bun install       # install dependencies
bun run dev       # dev server on 127.0.0.1:5173, proxies /api and /health to the daemon on :4242
bun run build     # writes dist/, which the Go binary embeds
```

`bun run test` runs Vitest under jsdom with a mocked `fetch`. `bun run check` runs svelte-check.

From the repo root, `make web` runs `bun install --frozen-lockfile && bun run build`, and `make e2e` builds
the UI and the binary, then runs the Playwright suite in `e2e/` (`bun run e2e`) against its own `coal serve`
on a fresh sandbox data dir. `bun run build` and `bun run test` first regenerate the Ajv validator from the
schema (`bun run gen:validator`); run that by hand after a schema change.
