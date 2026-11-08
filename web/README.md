# COAL web UI

The page served by `coal serve` at `http://127.0.0.1:4242`. Svelte 5 and Vite, built on
`@davenicoll/web-framework`.

The framework is not on a registry. `package.json` points at the sibling checkout with
`file:../../web-framework`, so clone it next to this repo (`../web-framework` from the repo root)
before installing. Bun installs it as a tree of symlinks, which is why `vite.config.ts` and
`tsconfig.json` set `preserveSymlinks`.

```sh
bun install       # install dependencies
bun run dev       # dev server on 127.0.0.1:5173, proxies /api and /health to the daemon on :4242
bun run build     # writes dist/, which the Go binary embeds
```

`bun run test` runs Vitest under jsdom with a mocked `fetch`. `bun run check` runs svelte-check.
