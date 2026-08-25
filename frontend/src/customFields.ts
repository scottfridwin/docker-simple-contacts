import type { CustomFieldType, CustomFieldValue } from './types';

export const MAX_CUSTOM_FIELDS = 64;
export const MAX_KEY_LENGTH = 64;
export const MAX_STRING_LENGTH = 1024;

const SNAKE_CASE = /^[a-z][a-z0-9]*(_[a-z0-9]+)*$/;

export interface DraftCustomField {
  key: string;
  type: CustomFieldType;
  value: string;
}

/** Validates a single custom field key against the snake_case policy. */
export function validateKey(key: string): string | null {
  if (!key) return 'Key is required';
  if (key.length > MAX_KEY_LENGTH) return `Key must be at most ${MAX_KEY_LENGTH} characters`;
  if (!SNAKE_CASE.test(key)) return 'Key must be lowercase snake_case';
  return null;
}

/** Validates a raw string value for the chosen type. */
export function validateValue(type: CustomFieldType, raw: string): string | null {
  switch (type) {
    case 'string':
      if (raw.length > MAX_STRING_LENGTH) return `Must be at most ${MAX_STRING_LENGTH} characters`;
      return null;
    case 'number':
      return raw.trim() !== '' && Number.isNaN(Number(raw)) ? 'Must be a valid number' : null;
    case 'boolean':
      return raw === 'true' || raw === 'false' ? null : 'Must be true or false';
    case 'date':
      return /^\d{4}-\d{2}-\d{2}$/.test(raw) ? null : 'Must be a date (YYYY-MM-DD)';
    default:
      return 'Unsupported type';
  }
}

/** Coerces a draft field's raw string into its typed API value. */
export function coerceValue(type: CustomFieldType, raw: string): CustomFieldValue {
  switch (type) {
    case 'number':
      return Number(raw);
    case 'boolean':
      return raw === 'true';
    default:
      return raw;
  }
}

/** Infers a display type from a stored custom field value. */
export function inferType(value: CustomFieldValue): CustomFieldType {
  if (typeof value === 'number') return 'number';
  if (typeof value === 'boolean') return 'boolean';
  if (typeof value === 'string' && /^\d{4}-\d{2}-\d{2}$/.test(value)) return 'date';
  return 'string';
}

export interface BuildResult {
  fields: Record<string, CustomFieldValue>;
  errors: Record<number, string>;
}

/** Builds the custom_fields payload from drafts, collecting per-row errors. */
export function buildCustomFields(drafts: DraftCustomField[]): BuildResult {
  const fields: Record<string, CustomFieldValue> = {};
  const errors: Record<number, string> = {};
  const seen = new Set<string>();

  drafts.forEach((draft, index) => {
    if (!draft.key && !draft.value) return;
    const keyErr = validateKey(draft.key);
    if (keyErr) {
      errors[index] = keyErr;
      return;
    }
    if (seen.has(draft.key)) {
      errors[index] = 'Duplicate key';
      return;
    }
    const valueErr = validateValue(draft.type, draft.value);
    if (valueErr) {
      errors[index] = valueErr;
      return;
    }
    seen.add(draft.key);
    fields[draft.key] = coerceValue(draft.type, draft.value);
  });

  return { fields, errors };
}
