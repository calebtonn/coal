import { type ChildProcess, execFileSync, spawn } from 'node:child_process';
import { mkdirSync, mkdtempSync, writeFileSync } from 'node:fs';
import { join, resolve } from 'node:path';
import { expect, test } from '@playwright/test';

const root = resolve(import.meta.dirname, '../..');
const sandbox = join(root, '.sandbox');
const bin = join(sandbox, 'bin', 'coal');
const port = 4343;
const base = `http://127.0.0.1:${port}`;

let server: ChildProcess | undefined;

test.beforeAll(async () => {
  const home = mkdtempSync(join(sandbox, 'e2e-'));
  const page = join(home, 'budget.html');
  writeFileSync(
    page,
    '<!doctype html><html><body><h1>Household budget</h1><p>Rent 1200, groceries 400.</p></body></html>\n',
  );
  const env = { ...process.env, COAL_HOME: join(home, 'coal') };
  execFileSync(bin, ['add', '--intent', 'Household budget tracker as a single HTML page', '--artifact', page], { env });
  server = spawn(bin, ['serve', '--port', String(port)], { env, stdio: 'inherit' });
  console.log(`coal serve pid ${server.pid}, COAL_HOME ${env.COAL_HOME}`);
  for (let i = 0; i < 50; i++) {
    try {
      if ((await fetch(`${base}/health`)).ok) return;
    } catch {}
    await new Promise((r) => setTimeout(r, 100));
  }
  throw new Error('coal serve did not answer /health');
});

test.afterAll(() => {
  if (server?.pid) process.kill(server.pid, 'SIGTERM');
});

test('the embedded UI shows the manifest and its HTML preview with no CSP violations', async ({ page }) => {
  const problems: string[] = [];
  page.on('console', (m) => problems.push(`console ${m.type()}: ${m.text().slice(0, 300)}`));
  page.on('pageerror', (e) => problems.push(`page error: ${e.message}`));
  await page.exposeFunction('reportViolation', (v: string) => problems.push(`csp violation: ${v}`));
  await page.addInitScript(() => {
    document.addEventListener('securitypolicyviolation', (e) => {
      (window as unknown as { reportViolation: (v: string) => void }).reportViolation(
        `${e.violatedDirective} blocked ${e.blockedURI || 'inline'} (${e.sourceFile}:${e.lineNumber}) ${e.sample}`,
      );
    });
  });

  const res = await page.goto('/');
  expect(res?.headers()['content-security-policy']).toContain("style-src 'self'");

  const results = page.getByRole('listbox', { name: 'Results' });
  await expect(results.getByRole('option')).toHaveCount(1);
  await expect(results).toContainText('Household budget tracker as a single HTML page');

  const detail = page.getByTestId('manifest-detail');
  await expect(detail).toContainText('Household budget tracker as a single HTML page');
  const iframe = detail.locator('iframe');
  await expect(iframe).toHaveAttribute('sandbox', '');
  await expect(page.frameLocator('[data-testid=manifest-detail] iframe').locator('h1')).toHaveText('Household budget');

  const search = page.getByRole('searchbox', { name: 'Search' });
  await search.fill('budgets');
  await expect(results.getByRole('option')).toHaveCount(1);
  await search.fill('zzzz');
  await expect(page.getByTestId('empty')).toHaveText('Nothing matches that. Try other words.');
  await search.fill('');
  await expect(results.getByRole('option')).toHaveCount(1);

  const html = page.locator('html');
  const toggle = page.getByTestId('theme-toggle');
  const before = await html.getAttribute('data-theme');
  for (const scheme of ['light', 'dark']) {
    await toggle.click();
    await expect(toggle).toHaveAttribute('data-scheme', scheme);
    await expect(html).toHaveAttribute('data-theme', new RegExp(`-${scheme}$`));
  }
  expect(await html.getAttribute('data-theme')).not.toBeNull();
  console.log(`theme before ${before}, after ${await html.getAttribute('data-theme')}`);
  await toggle.click();
  await page.reload();
  await expect(results.getByRole('option')).toHaveCount(1);

  const screens = join(sandbox, 'e2e-screens');
  mkdirSync(screens, { recursive: true });
  await page.screenshot({ path: join(screens, 'ui.png'), fullPage: true });

  expect(problems).toEqual([]);
});
