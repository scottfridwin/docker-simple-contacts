import { describe, expect, it } from 'vitest';
import {
  buildCustomFields,
  coerceValue,
  inferType,
  validateKey,
  validateValue,
} from '../src/customFields';

describe('validateKey', () => {
  it('accepts snake_case keys', () => {
    expect(validateKey('blood_type')).toBeNull();
    expect(validateKey('age')).toBeNull();
  });

  it('rejects invalid keys', () => {
    expect(validateKey('')).not.toBeNull();
    expect(validateKey('BloodType')).not.toBeNull();
    expect(validateKey('blood-type')).not.toBeNull();
    expect(validateKey('_leading')).not.toBeNull();
  });
});

describe('validateValue', () => {
  it('validates numbers', () => {
    expect(validateValue('number', '42')).toBeNull();
    expect(validateValue('number', 'abc')).not.toBeNull();
  });

  it('validates booleans', () => {
    expect(validateValue('boolean', 'true')).toBeNull();
    expect(validateValue('boolean', 'maybe')).not.toBeNull();
  });

  it('validates dates', () => {
    expect(validateValue('date', '2026-08-25')).toBeNull();
    expect(validateValue('date', 'not-a-date')).not.toBeNull();
  });
});

describe('coerceValue', () => {
  it('coerces to typed values', () => {
    expect(coerceValue('number', '42')).toBe(42);
    expect(coerceValue('boolean', 'true')).toBe(true);
    expect(coerceValue('string', 'hi')).toBe('hi');
  });
});

describe('inferType', () => {
  it('infers types from values', () => {
    expect(inferType(42)).toBe('number');
    expect(inferType(true)).toBe('boolean');
    expect(inferType('2026-08-25')).toBe('date');
    expect(inferType('hello')).toBe('string');
  });
});

describe('buildCustomFields', () => {
  it('builds a payload and reports errors', () => {
    const { fields, errors } = buildCustomFields([
      { key: 'blood_type', type: 'string', value: 'O+' },
      { key: 'age', type: 'number', value: '30' },
      { key: 'Bad Key', type: 'string', value: 'x' },
    ]);
    expect(fields).toEqual({ blood_type: 'O+', age: 30 });
    expect(errors[2]).toBeDefined();
  });

  it('flags duplicate keys', () => {
    const { errors } = buildCustomFields([
      { key: 'dup', type: 'string', value: 'a' },
      { key: 'dup', type: 'string', value: 'b' },
    ]);
    expect(errors[1]).toBe('Duplicate key');
  });
});
