import type { Manifest, SearchResult } from './types';

/** The daemon could not be reached at all (connection refused, proxy error). */
export class DaemonUnreachableError extends Error {}

async function get(path: string): Promise<Response> {
  let res: Response;
  try {
    res = await fetch(path, { headers: { Accept: 'application/json' } });
  } catch {
    throw new DaemonUnreachableError(path);
  }
  // The Vite dev proxy answers 502/504 when the daemon is down.
  if (res.status === 502 || res.status === 504) throw new DaemonUnreachableError(path);
  if (!res.ok) throw new Error(`${path} returned ${res.status}`);
  return res;
}

export async function searchManifests(q: string, status: string, limit = 50): Promise<SearchResult[]> {
  const params = new URLSearchParams({ q, ...(status ? { status } : {}), limit: String(limit) });
  const body = (await (await get(`/api/manifests?${params}`)).json()) as { results?: SearchResult[] };
  return body.results ?? [];
}

export async function getManifest(id: string): Promise<Manifest> {
  return (await get(`/api/manifests/${encodeURIComponent(id)}`)).json();
}

/** `sha256:<hex>` to the `/api/artifacts/<hex>` URL. */
export function artifactUrl(hash: string): string {
  return `/api/artifacts/${hash.replace(/^sha256:/, '')}`;
}

export const getArtifactText = async (hash: string) => (await get(artifactUrl(hash))).text();
