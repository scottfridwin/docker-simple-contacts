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
  nickname: string;
  pronouns: string;
  birthdate: string;
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
  const [middleNames, setMiddleNames] = useState((initial?.middle_names ?? []).join(', '));
  const [nickname, setNickname] = useState(initial?.nickname ?? '');
  const [pronouns, setPronouns] = useState(initial?.pronouns ?? '');
  const [birthdate, setBirthdate] = useState(initial?.birthdate ?? '');
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
      nickname: nickname.trim(),
      pronouns: pronouns.trim(),
      birthdate: birthdate.trim(),
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
        <label htmlFor="nickname">Nickname</label>
        <input id="nickname" value={nickname} onChange={(e) => setNickname(e.target.value)} />
      </div>

      <div className="field">
        <label htmlFor="pronouns">Pronouns</label>
        <input id="pronouns" value={pronouns} onChange={(e) => setPronouns(e.target.value)} />
      </div>

      <div className="field">
        <label htmlFor="birthdate">Birthdate</label>
        <input
          id="birthdate"
          type="date"
          value={birthdate}
          onChange={(e) => setBirthdate(e.target.value)}
        />
        {combinedErrors.birthdate && <span className="error">{combinedErrors.birthdate}</span>}
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
