import { render, screen, waitFor, within } from '@testing-library/svelte';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import App from './App.svelte';
import { budget, exporter, fakeDaemon } from './test/fixtures';

function searches(fetch: ReturnType<typeof vi.fn>): URLSearchParams[] {
  return fetch.mock.calls
    .map(([url]) => new URL(String(url), 'http://x'))
    .filter((u) => u.pathname === '/api/manifests')
    .map((u) => u.searchParams);
}

const rows = () => within(screen.getByRole('listbox', { name: 'Results' })).getAllByRole('option');

async function start(daemon = fakeDaemon()) {
  const fetch = vi.fn(daemon);
  vi.stubGlobal('fetch', fetch);
  const user = userEvent.setup();
  render(App);
  await screen.findByRole('listbox', { name: 'Results' });
  return { fetch, user };
}

describe('App', () => {
  it('lists everything on load and opens the newest result', async () => {
    const { fetch } = await start();
    expect(searches(fetch)[0].get('q')).toBe('');
    expect(await screen.findByTestId('manifest-detail')).toHaveTextContent(budget.intent.summary);
    await waitFor(() => expect(screen.getByText('valid')).toBeInTheDocument());
  });

  it('debounces search input', async () => {
    const { fetch, user } = await start();
    await user.type(screen.getByRole('searchbox'), 'budget');
    await waitFor(() => expect(searches(fetch).length).toBe(2));
    await new Promise((r) => setTimeout(r, 300));
    const all = searches(fetch);
    expect(all).toHaveLength(2);
    expect(all[1].get('q')).toBe('budget');
  });

  it('filters by status', async () => {
    const { fetch, user } = await start();
    await user.selectOptions(screen.getByLabelText('Status'), 'paused');
    await waitFor(() => expect(searches(fetch).at(-1)?.get('status')).toBe('paused'));
  });

  it('supports / to focus, arrows to move and Enter to open', async () => {
    const { user } = await start();
    await screen.findByTestId('manifest-detail');
    (document.activeElement as HTMLElement | null)?.blur();

    await user.keyboard('/');
    const search = screen.getByRole('searchbox');
    expect(search).toHaveFocus();
    expect(search).toHaveValue('');

    await user.keyboard('{ArrowDown}');
    expect(rows()[1]).toHaveAttribute('aria-selected', 'true');
    await user.keyboard('{ArrowDown}{ArrowDown}{ArrowUp}');
    expect(rows()[1]).toHaveAttribute('aria-selected', 'true');
    expect(search).toHaveAttribute('aria-activedescendant', `result-${exporter.id}`);

    await user.keyboard('{Enter}');
    await waitFor(() =>
      expect(screen.getByTestId('manifest-detail')).toHaveTextContent(exporter.intent.summary),
    );
  });

  it('shows the empty state', async () => {
    vi.stubGlobal('fetch', vi.fn(fakeDaemon([])));
    render(App);
    expect(await screen.findByTestId('empty')).toHaveTextContent(
      'Nothing recorded yet. Work captured by COAL hooks or added with coal add appears here.',
    );
  });

  it('says so when the daemon is not running', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError('Failed to fetch')));
    render(App);
    expect(
      await screen.findByText('The COAL daemon is not running. Start it with coal serve.'),
    ).toBeInTheDocument();
  });
});
