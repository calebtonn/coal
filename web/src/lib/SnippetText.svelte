<script lang="ts">
  import { parseSnippet } from './format';

  let { snippet }: { snippet: string } = $props();
  const parts = $derived(parseSnippet(snippet));
</script>

<!-- prettier-ignore -->
<span class="text-base-content/70 text-sm">{#each parts as part, i (i)}{#if part.bold}<b class="text-base-content font-semibold">{part.text}</b>{:else}{part.text}{/if}{/each}</span>
