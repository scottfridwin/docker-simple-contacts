import { useState } from 'react';
import type { CustomFieldType, Person } from '../types';
import {
  buildCustomFields,
  inferType,
  MAX_CUSTOM_FIELDS,
  type DraftCustomField,
} from '../customFields';

export interface PersonFormValues {
  first_name: string;
  middle_names: string[];
  last_name: string;
  display_name: string;
  custom_fields: Record<string, string | number | boolean>;
}

interface PersonFormProps {
  initial?: Person;
  submitting?: boolean;
  serverErrors?: Record<string, string>;
  onSubmit: (values: PersonFormValues) => void;
  onCancel: () => void;
}

function draftsFromPerson(person?: Person): DraftCustomField[] {
  if (!person) return [];
  return Object.entries(person.custom_fields ?? {}).map(([key, value]) => ({
    key,
    type: inferType(value),
    value: String(value),
  }));
}

// Mirrors the backend's display-name derivation so the form can tell whether a
// stored display name is a custom override or just the derived default.
function deriveDisplayName(first: string, middles: string[], last: string): string {
  return [first, ...middles, last]
    .map((s) => s.trim())
    .filter(Boolean)
    .join(' ');
}

// Returns the stored display name only when it is a custom override; otherwise
// an empty string so the field stays "auto" and the backend re-derives on save.
function initialCustomDisplayName(person?: Person): string {
  if (!person) return '';
  const derived = deriveDisplayName(person.first_name, person.middle_names ?? [], person.last_name);
  return (person.display_name ?? '') === derived ? '' : (person.display_name ?? '');
}

const TYPES: CustomFieldType[] = ['string', 'number', 'boolean', 'date'];

export function PersonForm({
  initial,
  submitting,
  serverErrors,
  onSubmit,
  onCancel,
}: PersonFormProps) {
  const [firstName, setFirstName] = useState(initial?.first_name ?? '');
  const [lastName, setLastName] = useState(initial?.last_name ?? '');
  const [displayName, setDisplayName] = useState(initialCustomDisplayName(initial));
  const [middleNames, setMiddleNames] = useState((initial?.middle_names ?? []).join(', '));
  const [drafts, setDrafts] = useState<DraftCustomField[]>(draftsFromPerson(initial));
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [fieldErrors, setFieldErrors] = useState<Record<number, string>>({});

  const updateDraft = (index: number, patch: Partial<DraftCustomField>) => {
    setDrafts((prev) => prev.map((d, i) => (i === index ? { ...d, ...patch } : d)));
  };

  const addDraft = () => {
    if (drafts.length >= MAX_CUSTOM_FIELDS) return;
    setDrafts((prev) => [...prev, { key: '', type: 'string', value: '' }]);
  };

  const removeDraft = (index: number) => {
    setDrafts((prev) => prev.filter((_, i) => i !== index));
  };

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    const nextErrors: Record<string, string> = {};
    if (!firstName.trim()) nextErrors.first_name = 'First name is required';
    if (!lastName.trim()) nextErrors.last_name = 'Last name is required';

    const { fields, errors: draftErrors } = buildCustomFields(drafts);
    setFieldErrors(draftErrors);
    setErrors(nextErrors);

    if (Object.keys(nextErrors).length > 0 || Object.keys(draftErrors).length > 0) {
      return;
    }

    onSubmit({
      first_name: firstName.trim(),
      last_name: lastName.trim(),
      display_name: displayName.trim(),
      middle_names: middleNames
        .split(',')
        .map((s) => s.trim())
        .filter(Boolean),
      custom_fields: fields,
    });
  };

  const combinedErrors = { ...errors, ...serverErrors };

  return (
    <form onSubmit={handleSubmit} className="person-form" aria-label="person form">
      <div className="field">
        <label htmlFor="first_name">First name *</label>
        <input
          id="first_name"
          value={firstName}
          onChange={(e) => setFirstName(e.target.value)}
          aria-required="true"
        />
        {combinedErrors.first_name && <span className="error">{combinedErrors.first_name}</span>}
      </div>

      <div className="field">
        <label htmlFor="middle_names">Middle names (comma separated, in order)</label>
        <input
          id="middle_names"
          value={middleNames}
          onChange={(e) => setMiddleNames(e.target.value)}
        />
      </div>

      <div className="field">
        <label htmlFor="last_name">Last name *</label>
        <input
          id="last_name"
          value={lastName}
          onChange={(e) => setLastName(e.target.value)}
          aria-required="true"
        />
        {combinedErrors.last_name && <span className="error">{combinedErrors.last_name}</span>}
      </div>

      <div className="field">
        <label htmlFor="display_name">Display name (optional, derived if blank)</label>
        <input
          id="display_name"
          value={displayName}
          onChange={(e) => setDisplayName(e.target.value)}
        />
      </div>

      <fieldset className="custom-fields">
        <legend>Custom fields</legend>
        {drafts.map((draft, index) => (
          <div className="custom-field-row" key={index}>
            <input
              aria-label={`custom field key ${index}`}
              placeholder="key_name"
              value={draft.key}
              onChange={(e) => updateDraft(index, { key: e.target.value })}
            />
            <select
              aria-label={`custom field type ${index}`}
              value={draft.type}
              onChange={(e) => updateDraft(index, { type: e.target.value as CustomFieldType })}
            >
              {TYPES.map((t) => (
                <option key={t} value={t}>
                  {t}
                </option>
              ))}
            </select>
            {draft.type === 'boolean' ? (
              <select
                aria-label={`custom field value ${index}`}
                value={draft.value || 'true'}
                onChange={(e) => updateDraft(index, { value: e.target.value })}
              >
                <option value="true">true</option>
                <option value="false">false</option>
              </select>
            ) : (
              <input
                aria-label={`custom field value ${index}`}
                type={draft.type === 'date' ? 'date' : draft.type === 'number' ? 'number' : 'text'}
                value={draft.value}
                onChange={(e) => updateDraft(index, { value: e.target.value })}
              />
            )}
            <button
              type="button"
              onClick={() => removeDraft(index)}
              aria-label={`remove field ${index}`}
            >
              Remove
            </button>
            {fieldErrors[index] && <span className="error">{fieldErrors[index]}</span>}
          </div>
        ))}
        <button type="button" onClick={addDraft} disabled={drafts.length >= MAX_CUSTOM_FIELDS}>
          Add custom field
        </button>
      </fieldset>

      <div className="actions">
        <button type="submit" disabled={submitting}>
          {submitting ? 'Saving…' : 'Save'}
        </button>
        <button type="button" onClick={onCancel}>
          Cancel
        </button>
      </div>
    </form>
  );
}
