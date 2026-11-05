import { render, screen, within } from '@testing-library/svelte';
import { describe, expect, it, vi } from 'vitest';
import { NOW, results } from '../test/fixtures';
import { parseSnippet, relativeTime } from './format';
import ResultsList from './ResultsList.svelte';

describe('ResultsList', () => {
  it('shows summary, status badge, relative time and artifact count', () => {
    render(ResultsList, { results, selected: 0, now: NOW, onpick: () => {} });
    const rows = screen.getAllByRole('option');
    expect(rows).toHaveLength(3);
    const first = within(rows[0]);
    expect(first.getByText('Household budget tracker as a single HTML page')).toBeInTheDocument();
    expect(first.getByText('active')).toHaveClass('badge-success');
    expect(first.getByText('3 days ago')).toBeInTheDocument();
    expect(first.getByText('1 artifact')).toBeInTheDocument();
    expect(within(rows[1]).getByText('paused')).toHaveClass('badge-warning');
    expect(rows[1]).toHaveTextContent(/2 hours ago\s*2 artifacts/);
    expect(within(rows[2]).getByText('abandoned')).toHaveClass('badge-ghost');
    expect(rows[0]).toHaveAttribute('aria-selected', 'true');
  });

  it('bolds snippet matches and keeps any other markup as text', () => {
    render(ResultsList, { results, selected: 0, now: NOW, onpick: () => {} });
    const row = screen.getAllByRole('option')[0];
    const bold = row.querySelectorAll('b');
    expect(bold).toHaveLength(1);
    expect(bold[0]).toHaveTextContent('budget');
    expect(row.querySelector('img')).toBeNull();
    expect(row).toHaveTextContent('<img src=x onerror="window.pwned=1">');
    expect((window as { pwned?: number }).pwned).toBeUndefined();
  });

  it('calls onpick with the row index on click', async () => {
    const onpick = vi.fn();
    render(ResultsList, { results, selected: 0, now: NOW, onpick });
    screen.getAllByRole('option')[2].click();
    expect(onpick).toHaveBeenCalledWith(2);
  });
});

describe('format', () => {
  it('splits snippets on b tags only', () => {
    expect(parseSnippet('a <b>b</b> <i>c</i>')).toEqual([
      { text: 'a ', bold: false },
      { text: 'b', bold: true },
      { text: ' <i>c</i>', bold: false },
    ]);
  });

  it('formats relative times', () => {
    expect(relativeTime('2026-11-04T02:41:10Z', NOW)).toBe('just now');
    expect(relativeTime('2026-11-04T02:30:33Z', NOW)).toBe('11 minutes ago');
    expect(relativeTime('2026-10-04T02:41:33Z', NOW)).toBe('last month');
  });
});
