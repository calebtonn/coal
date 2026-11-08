import validate from '../generated/validate-manifest.js';

export interface Validity {
  valid: boolean;
  error?: string;
}

/** Check a manifest against the schema with the validator generated at build time. */
export function validateManifest(manifest: unknown): Validity {
  if (validate(manifest)) return { valid: true };
  const first = validate.errors?.[0];
  return { valid: false, error: first ? `${first.instancePath || '/'} ${first.message ?? 'is invalid'}` : 'is invalid' };
}
