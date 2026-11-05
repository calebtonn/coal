<script lang="ts">
  import { Select } from '@davenicoll/web-framework';
  import { artifactUrl, getArtifactText } from './api';
  import { formatSize, relativeTime } from './format';
  import type { Artifact, ArtifactVersion } from './types';

  let { artifact }: { artifact: Artifact } = $props();

  const versions = $derived<ArtifactVersion[]>(
    artifact.versions?.length
      ? artifact.versions
      : [{ hash: artifact.hash, size: artifact.size, createdAt: artifact.createdAt ?? '' }],
  );
  // The parent keys this component on the artifact, so the initial value is the latest version.
  // svelte-ignore state_referenced_locally
  let picked = $state(String(versions.length - 1));
  const version = $derived(versions[Number(picked)] ?? versions[versions.length - 1]);
  const url = $derived(artifactUrl(version.hash));
  const title = $derived(artifact.title ?? artifact.uri ?? 'Artifact');
  const meta = $derived(
    [artifact.type, artifact.mime, version.size === undefined ? '' : formatSize(version.size)].filter(Boolean).join(', '),
  );
  const skipped = $derived(version.skipped ?? artifact.skipped);
  const isText = $derived(['code', 'doc', 'data'].includes(artifact.type));

  let text = $state<string | null>(null);
  let textError = $state<string | null>(null);

  $effect(() => {
    if (!isText || skipped) return;
    const hash = version.hash;
    let stale = false;
    text = null;
    textError = null;
    getArtifactText(hash).then((body) => !stale && (text = body), () => !stale && (textError = 'Could not load this artifact.'));
    return () => (stale = true);
  });
</script>

<div class="flex flex-col gap-3" data-testid="artifact-view" data-hash={version.hash}>
  <div class="text-base-content/70 flex flex-wrap items-end justify-between gap-3 text-sm">
    <div>
      <div class="text-base-content font-medium">{title}</div>
      <div>{meta}</div>
      {#if artifact.uri}<div class="break-all text-xs">{artifact.uri}</div>{/if}
    </div>
    {#if versions.length > 1}
      <Select
        label="Version"
        size="sm"
        class="w-56"
        bind:value={picked}
        options={versions.map((v, i) => ({
          value: String(i),
          label: `Version ${i + 1}${i === versions.length - 1 ? ' (latest)' : ''}, ${relativeTime(v.createdAt)}`,
        }))}
      />
    {/if}
  </div>

  {#if skipped}
    <p class="text-sm" data-testid="artifact-skipped">The body was not stored: {skipped}</p>
  {:else if artifact.type === 'html'}
    <iframe
      sandbox=""
      src={url}
      {title}
      class="rounded-box border-base-300 h-[32rem] w-full border bg-white"
    ></iframe>
  {:else if isText}
    {#if textError}
      <p class="text-error text-sm">{textError}</p>
    {:else if text === null}
      <p class="text-sm">Loading</p>
    {:else}
      <pre
        class="bg-base-200 rounded-box max-h-[32rem] overflow-auto p-3 text-xs"
        data-testid="artifact-text">{text}</pre>
    {/if}
  {:else if artifact.type === 'image'}
    <img src={url} alt={title} class="rounded-box max-h-[32rem] max-w-full object-contain" />
  {:else}
    <a class="link" href={url} download={artifact.title}>Download {title}</a>
  {/if}
</div>
