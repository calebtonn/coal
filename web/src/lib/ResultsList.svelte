<script lang="ts">
  import { relativeTime } from './format';
  import SnippetText from './SnippetText.svelte';
  import StatusBadge from './StatusBadge.svelte';
  import type { SearchResult } from './types';

  interface Props {
    results: SearchResult[];
    selected: number;
    openId?: string | null;
    now?: Date;
    onpick: (index: number) => void;
  }

  let { results, selected, openId = null, now = new Date(), onpick }: Props = $props();

  $effect(() => {
    const id = results[selected]?.id;
    if (id) document.getElementById(`result-${id}`)?.scrollIntoView?.({ block: 'nearest' });
  });
</script>

<ul id="results" role="listbox" aria-label="Results" class="flex flex-col gap-1">
  {#each results as r, i (r.id)}
    <li
      id="result-{r.id}"
      role="option"
      aria-selected={i === selected}
      aria-current={r.id === openId ? 'true' : undefined}
      tabindex="-1"
      class={[
        'cursor-pointer rounded-box border px-3 py-2',
        i === selected ? 'border-primary bg-base-200' : 'border-base-300 hover:bg-base-200/60',
      ]}
      onclick={() => onpick(i)}
      onkeydown={(e) => e.key === 'Enter' && onpick(i)}
    >
      <div class="flex items-start justify-between gap-2">
        <span class="font-medium">{r.summary}</span>
        <StatusBadge status={r.status} />
      </div>
      <div class="text-base-content/60 mt-1 flex gap-3 text-xs">
        <time datetime={r.lastActivityAt} title={r.lastActivityAt}>{relativeTime(r.lastActivityAt, now)}</time>
        <span>{r.artifactCount} {r.artifactCount === 1 ? 'artifact' : 'artifacts'}</span>
      </div>
      {#if r.snippet}
        <div class="mt-1"><SnippetText snippet={r.snippet} /></div>
      {/if}
    </li>
  {/each}
</ul>
