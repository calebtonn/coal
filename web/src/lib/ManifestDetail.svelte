<script lang="ts">
  import { Badge, Card, Tabs, type TabItem } from '@davenicoll/web-framework';
  import ArtifactView from './ArtifactView.svelte';
  import { relativeTime } from './format';
  import StatusBadge from './StatusBadge.svelte';
  import type { Manifest } from './types';
  import type { Validity } from './validate';

  let { manifest, validity = null }: { manifest: Manifest; validity?: Validity | null } = $props();

  const tabs = $derived<TabItem[]>(
    manifest.artifacts.map((a, i) => ({ id: `artifact-${i}`, label: a.title ?? a.type })),
  );
  let active = $state('artifact-0');
  const source = $derived(manifest.intent.source ?? 'model');
</script>

<Card title={manifest.intent.summary} data-testid="manifest-detail">
  <div class="flex flex-wrap items-center gap-2 text-sm">
    <StatusBadge status={manifest.status} />
    <Badge appearance="outline" size="sm">{source === 'user' ? 'In your words' : 'Written by the model'}</Badge>
    {#if validity}
      <span title={validity.error}>
        <Badge variant={validity.valid ? 'success' : 'error'} appearance="soft" size="sm">{validity.valid ? 'valid' : 'invalid'}</Badge>
      </span>
    {/if}
    <span class="text-base-content/60">
      Last activity
      <time datetime={manifest.state.lastActivityAt}>{relativeTime(manifest.state.lastActivityAt)}</time>
    </span>
  </div>
  {#if validity && !validity.valid}
    <p class="text-error text-xs" data-testid="validity-error">{validity.error}</p>
  {/if}

  {#if manifest.intent.goal}
    <section>
      <h3 class="text-sm font-semibold">Goal</h3>
      <p>{manifest.intent.goal}</p>
    </section>
  {/if}

  {#if manifest.intent.tags?.length}
    <div class="flex flex-wrap gap-1">
      {#each manifest.intent.tags as tag (tag)}<Badge appearance="soft" size="sm">{tag}</Badge>{/each}
    </div>
  {/if}

  <section>
    <h3 class="text-sm font-semibold">Where it stands</h3>
    <dl class="grid grid-cols-[max-content_1fr] gap-x-4 gap-y-1 text-sm">
      {#if manifest.state.nextStep}
        <dt class="text-base-content/60">Next step</dt>
        <dd>{manifest.state.nextStep}</dd>
      {/if}
      {#if manifest.state.openQuestions?.length}
        <dt class="text-base-content/60">Open questions</dt>
        <dd>
          <ul class="list-disc pl-4">
            {#each manifest.state.openQuestions as q, i (i)}<li>{q}</li>{/each}
          </ul>
        </dd>
      {/if}
      {#if manifest.state.summary}
        <dt class="text-base-content/60">Summary</dt>
        <dd>{manifest.state.summary}</dd>
      {/if}
      <dt class="text-base-content/60">Recorded by</dt>
      <dd>{manifest.provenance.tool}{manifest.provenance.model ? `, ${manifest.provenance.model}` : ''}</dd>
    </dl>
  </section>

  {#if manifest.contexts?.length}
    <section>
      <h3 class="text-sm font-semibold">Contexts</h3>
      <ul class="text-sm">
        {#each manifest.contexts as c, i (i)}
          <li><span class="text-base-content/60">{c.kind}</span> {c.label ?? c.key}
            {#if c.label}<span class="text-base-content/60 break-all text-xs">{c.key}</span>{/if}</li>
        {/each}
      </ul>
    </section>
  {/if}

  <section>
    <h3 class="mb-2 text-sm font-semibold">Artifacts</h3>
    {#if tabs.length}
      <Tabs {tabs} bind:active label="Artifacts">
        {#snippet children(id)}
          {@const artifact = manifest.artifacts[Number(id.slice('artifact-'.length))]}
          {#if artifact}
            {#key `${id}:${artifact.uri ?? artifact.hash}`}
              <div class="pt-3"><ArtifactView {artifact} /></div>
            {/key}
          {/if}
        {/snippet}
      </Tabs>
    {:else}
      <p class="text-sm">No artifacts registered.</p>
    {/if}
  </section>
</Card>
