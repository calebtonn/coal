<script lang="ts">
  import Archive from '@lucide/svelte/icons/archive';
  import Search from '@lucide/svelte/icons/search';
  import { Alert, AppShell, Input, Page, PageHeader, Select } from '@davenicoll/web-framework';
  import { ThemeSelect, ThemeToggle, type NavSection } from '@davenicoll/web-framework';
  import { onMount } from 'svelte';
  import { DaemonUnreachableError, getManifest, getSchema, searchManifests } from './lib/api';
  import ManifestDetail from './lib/ManifestDetail.svelte';
  import ResultsList from './lib/ResultsList.svelte';
  import type { Manifest, SearchResult } from './lib/types';
  import { compileValidator, type Validator } from './lib/validate';

  const SEARCH_DEBOUNCE_MS = 200;
  const SEARCH_ID = 'coal-search';
  const DAEMON_DOWN = 'The COAL daemon is not running. Start it with coal serve.';

  const nav: NavSection[] = [{ items: [{ label: 'Records', href: '/', icon: Archive }] }];
  const statusOptions = [{ value: '', label: 'All statuses' }].concat(
    ['active', 'paused', 'done', 'abandoned'].map((v) => ({ value: v, label: v })),
  );

  let query = $state('');
  let status = $state('');
  let results = $state<SearchResult[]>([]);
  let loaded = $state(false);
  let error = $state<string | null>(null);
  let selected = $state(0);
  let openId = $state<string | null>(null);
  let manifest = $state<Manifest | null>(null);
  let validator = $state<Validator | null>(null);

  const validity = $derived(validator && manifest ? validator(manifest) : null);
  const filtered = $derived(query.trim() !== '' || status !== '');

  const describe = (err: unknown) =>
    err instanceof DaemonUnreachableError ? DAEMON_DOWN : `The daemon returned an error: ${(err as Error).message}`;

  let searchSeq = 0;
  let firstSearch = true;
  $effect(() => {
    const q = query.trim();
    const s = status;
    const seq = ++searchSeq;
    const timer = setTimeout(async () => {
      try {
        const found = await searchManifests(q, s);
        if (seq !== searchSeq) return;
        results = found;
        error = null;
        selected = 0;
        if (openId === null && found.length) open(found[0].id);
      } catch (err) {
        if (seq === searchSeq) error = describe(err);
      } finally {
        if (seq === searchSeq) loaded = true;
      }
    }, firstSearch ? 0 : SEARCH_DEBOUNCE_MS);
    firstSearch = false;
    return () => clearTimeout(timer);
  });

  let openSeq = 0;
  async function open(id: string) {
    const seq = ++openSeq;
    openId = id;
    try {
      const m = await getManifest(id);
      if (seq === openSeq) manifest = m;
    } catch (err) {
      if (seq === openSeq) error = describe(err);
    }
  }

  function pick(index: number) {
    selected = index;
    const r = results[index];
    if (r) open(r.id);
  }

  onMount(() => void getSchema().then((schema) => (validator = compileValidator(schema)), () => {}));

  const isTyping = (el: Element | null) =>
    !!el && (['INPUT', 'TEXTAREA', 'SELECT'].includes(el.tagName) || (el as HTMLElement).isContentEditable);

  function onKeydown(e: KeyboardEvent) {
    if (e.defaultPrevented || e.altKey || e.ctrlKey || e.metaKey) return;
    const target = e.target as Element | null;
    if (e.key === '/' && !isTyping(target)) {
      e.preventDefault();
      document.getElementById(SEARCH_ID)?.focus();
      return;
    }
    // Arrows and Enter drive the list from the search box, the list itself, or the page body.
    const inList =
      !target || target === document.body || target.id === SEARCH_ID || !!target.closest?.('#results');
    if (!inList || !results.length) return;
    if (e.key === 'ArrowDown') {
      e.preventDefault();
      selected = Math.min(selected + 1, results.length - 1);
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      selected = Math.max(selected - 1, 0);
    } else if (e.key === 'Enter') {
      e.preventDefault();
      pick(selected);
    }
  }
</script>

<svelte:window onkeydown={onKeydown} />

<AppShell {nav} title="COAL" currentPath="/" onNavigate={() => {}}>
  {#snippet topnavEnd()}
    <ThemeSelect />
    <ThemeToggle />
  {/snippet}

  <Page>
    <PageHeader title="Records" description="A record of your work" />

    {#if error}
      <Alert type="error">{error}</Alert>
    {/if}

    <div class="grid gap-6 lg:grid-cols-[minmax(18rem,2fr)_3fr]">
      <div class="flex flex-col gap-3">
        <div class="flex gap-2">
          <Input
            id={SEARCH_ID}
            type="search"
            label="Search"
            placeholder="Find by what it was for"
            icon={Search}
            class="flex-1"
            autocomplete="off"
            aria-controls="results"
            aria-activedescendant={results[selected] ? `result-${results[selected].id}` : undefined}
            bind:value={query}
          />
          <Select label="Status" options={statusOptions} bind:value={status} class="w-40" />
        </div>

        {#if !loaded}
          <p class="text-sm">Loading</p>
        {:else if !error && results.length === 0}
          <p class="text-base-content/70 text-sm" data-testid="empty">
            {filtered
              ? 'Nothing matches that. Try other words.'
              : 'Nothing recorded yet. Work captured by COAL hooks or added with coal add appears here.'}
          </p>
        {:else}
          <ResultsList {results} {selected} {openId} onpick={pick} />
        {/if}
      </div>

      <div>
        {#if manifest}
          {#key manifest.id}
            <ManifestDetail {manifest} {validity} />
          {/key}
        {/if}
      </div>
    </div>
  </Page>
</AppShell>
