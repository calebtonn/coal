import Ajv2020 from 'ajv/dist/2020';
import addFormats from 'ajv-formats';

export interface Validity {
  valid: boolean;
  error?: string;
}

export type Validator = (manifest: unknown) => Validity;

/** Compile the Intent Manifest schema (JSON Schema 2020-12) once. */
export function compileValidator(schema: object): Validator {
  const ajv = new Ajv2020({ allErrors: false, strict: false });
  addFormats(ajv);
  const check = ajv.compile(schema);
  return (manifest) => {
    if (check(manifest)) return { valid: true };
    const first = check.errors?.[0];
    const where = first?.instancePath || '/';
    return { valid: false, error: first ? `${where} ${first.message ?? 'is invalid'}` : 'is invalid' };
  };
}
