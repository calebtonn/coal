import example from '../../../spec/examples/budget-tracker.manifest.json';
import type { Manifest, SearchResult } from '../lib/types';

export const budget = example as unknown as Manifest;

const hex = (c: string) => c.repeat(64);

/** A second manifest with an HTML page and a script that was written twice. */
export const exporter: Manifest = {
  ...budget,
  id: '019a4000-0000-7000-8000-000000000002',
  intent: { summary: 'CSV exporter for the budget page', source: 'model', tags: ['csv'] },
  status: 'paused',
  artifacts: [
    {
      hash: `sha256:${hex('a')}`,
      title: 'preview.html',
      type: 'html',
      mime: 'text/html',
      versions: [{ hash: `sha256:${hex('a')}`, size: 10, createdAt: '2026-11-02T10:00:00Z' }],
    },
    {
      hash: `sha256:${hex('c')}`,
      title: 'export.js',
      type: 'code',
      mime: 'text/javascript',
      uri: 'file:///tmp/budget/export.js',
      versions: [
        { hash: `sha256:${hex('b')}`, size: 5, createdAt: '2026-11-02T10:00:00Z' },
        { hash: `sha256:${hex('c')}`, size: 7, createdAt: '2026-11-02T11:00:00Z' },
      ],
    },
  ],
};

export const NOW = new Date('2026-11-04T02:41:33Z');

export const results: SearchResult[] = [
  {
    id: budget.id,
    summary: budget.intent.summary,
    source: 'user',
    status: 'active',
    lastActivityAt: '2026-11-01T02:41:33Z',
    updatedAt: '2026-11-01T02:41:33Z',
    artifactCount: 1,
    nextStep: 'Add CSV export and a category edit dialog',
    snippet: 'Household <b>budget</b> tracker <img src=x onerror="window.pwned=1">',
    matched: ['summary'],
  },
  { id: exporter.id, summary: exporter.intent.summary, source: 'model', status: 'paused',
    lastActivityAt: '2026-11-04T00:41:33Z', updatedAt: '2026-11-04T00:41:33Z', artifactCount: 2,
    nextStep: null, snippet: null, matched: [] },
  { id: '019a4000-0000-7000-8000-000000000003', summary: 'Old landing page', source: 'user', status: 'abandoned',
    lastActivityAt: '2026-09-01T00:00:00Z', updatedAt: '2026-09-01T00:00:00Z', artifactCount: 0,
    nextStep: null, snippet: null, matched: [] },
];

const json = (body: unknown) =>
  new Response(JSON.stringify(body), { headers: { 'Content-Type': 'application/json' } });

/** A fetch stand-in that answers the daemon API from the fixtures. Nothing touches the network. */
export function fakeDaemon(list: SearchResult[] = results) {
  const manifests = new Map([budget, exporter].map((m) => [m.id, m]));
  return async (input: RequestInfo | URL): Promise<Response> => {
    const url = new URL(String(input), 'http://127.0.0.1:4242');
    if (url.pathname === '/api/manifests') return json({ results: list });
    if (url.pathname.startsWith('/api/manifests/')) {
      const m = manifests.get(decodeURIComponent(url.pathname.slice('/api/manifests/'.length)));
      return m ? json(m) : new Response('not found', { status: 404 });
    }
    if (url.pathname.startsWith('/api/artifacts/')) {
      return new Response(`body of ${url.pathname.slice(15, 19)}`);
    }
    return new Response('not found', { status: 404 });
  };
}
