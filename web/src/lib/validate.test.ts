// @vitest-environment node
import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';
import { generateValidatorSource, OUTPUT_PATH } from '../../scripts/gen-validator';
import { budget } from '../test/fixtures';
import { validateManifest } from './validate';

describe('generated validator', () => {
  it('matches what the schema generates now', () => {
    expect(readFileSync(OUTPUT_PATH, 'utf8')).toBe(generateValidatorSource());
  });

  it('compiles no code at runtime', () => {
    expect(readFileSync(OUTPUT_PATH, 'utf8')).not.toMatch(/new Function|\beval\(|require\(/);
  });

  it('accepts the example and reports the first error', () => {
    expect(validateManifest(budget)).toEqual({ valid: true });
    const broken = { ...budget, id: 'not-a-uuid' };
    expect(validateManifest(broken)).toMatchObject({ valid: false, error: expect.stringMatching(/^\/id /) });
  });
});
