import { render, screen, waitFor } from '@testing-library/svelte';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import { budget, exporter, fakeDaemon } from '../test/fixtures';
import ManifestDetail from './ManifestDetail.svelte';
import { validateManifest as validate } from './validate';

describe('ManifestDetail', () => {
  it('shows intent, state, provenance and contexts', () => {
    render(ManifestDetail, { manifest: budget });
    expect(screen.getByText(budget.intent.summary)).toBeInTheDocument();
    expect(screen.getByText('In your words')).toBeInTheDocument();
    expect(screen.getByText(budget.intent.goal!)).toBeInTheDocument();
    expect(screen.getByText('personal-finance')).toBeInTheDocument();
    expect(screen.getByText('Add CSV export and a category edit dialog')).toBeInTheDocument();
    expect(screen.getByText(budget.state.openQuestions![0])).toBeInTheDocument();
    expect(screen.getByText('claude-code, claude-opus-5-5')).toBeInTheDocument();
    expect(screen.getByText('budget repo')).toBeInTheDocument();
  });

  it('renders html in an iframe with an empty sandbox', () => {
    render(ManifestDetail, { manifest: budget });
    const frame = document.querySelector('iframe')!;
    expect(frame.getAttribute('sandbox')).toBe('');
    expect(frame.getAttribute('src')).toBe(`/api/artifacts/${budget.artifacts[0].hash.slice(7)}`);
    expect(frame).toHaveAttribute('title', 'budget.html');
  });

  it('switches artifact tabs and defaults to the latest version', async () => {
    const fetch = vi.fn(fakeDaemon());
    vi.stubGlobal('fetch', fetch);
    const user = userEvent.setup();
    render(ManifestDetail, { manifest: exporter });
    expect(screen.getByRole('tab', { name: 'preview.html' })).toHaveAttribute('aria-selected', 'true');

    await user.click(screen.getByRole('tab', { name: 'export.js' }));
    expect(document.querySelector('iframe')).toBeNull();
    const picker = screen.getByLabelText('Version') as HTMLSelectElement;
    expect(picker.value).toBe('1');
    expect(picker.selectedOptions[0]).toHaveTextContent('Version 2 (latest)');
    expect(await screen.findByTestId('artifact-text')).toHaveTextContent('body of cccc');
    expect(fetch).toHaveBeenCalledWith(`/api/artifacts/${'c'.repeat(64)}`, expect.anything());

    await user.selectOptions(picker, '0');
    await waitFor(() => expect(screen.getByTestId('artifact-text')).toHaveTextContent('body of bbbb'));
  });

  it('says when the selected version body was skipped', async () => {
    const [html, code] = exporter.artifacts;
    const versions = [{ ...code.versions![0], skipped: 'matched a token pattern' }, code.versions![1]];
    const manifest = { ...exporter, artifacts: [html, { ...code, versions }] };
    vi.stubGlobal('fetch', vi.fn(fakeDaemon()));
    const user = userEvent.setup();
    render(ManifestDetail, { manifest });
    await user.click(screen.getByRole('tab', { name: 'export.js' }));
    expect(await screen.findByTestId('artifact-text')).toHaveTextContent('body of cccc');
    await user.selectOptions(screen.getByLabelText('Version'), '0');
    expect(screen.getByTestId('artifact-skipped')).toHaveTextContent('The body was not stored: matched a token pattern');
    expect(screen.queryByTestId('artifact-text')).toBeNull();
  });

  it('shows valid or invalid against the schema', () => {
    const { unmount } = render(ManifestDetail, { manifest: budget, validity: validate(budget) });
    expect(screen.getByText('valid')).toBeInTheDocument();
    unmount();

    const broken = { ...budget, status: 'lost' } as unknown as typeof budget;
    render(ManifestDetail, { manifest: broken, validity: validate(broken) });
    expect(screen.getByText('invalid')).toBeInTheDocument();
    expect(screen.getByTestId('validity-error')).toHaveTextContent('/status');
  });
});
