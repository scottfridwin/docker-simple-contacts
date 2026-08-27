import { useState } from 'react';
import type { Address, AddressType, CustomFieldType, Person } from '../types';
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
  phone_numbers: string[];
  addresses: Address[];
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

function StringListField({
  label,
  values,
  onChange,
  maxItems,
  inputType = 'text',
}: {
  label: string;
  values: string[];
  onChange: (values: string[]) => void;
  maxItems?: number;
  inputType?: string;
}) {
  const update = (i: number, v: string) => onChange(values.map((x, j) => (j === i ? v : x)));
  const remove = (i: number) => onChange(values.filter((_, j) => j !== i));
  const add = () => onChange([...values, '']);
  const canAdd = maxItems === undefined || values.length < maxItems;

  return (
    <div className="array-field">
      <span className="array-field-label">{label}</span>
      {values.map((v, i) => (
        <div className="array-field-row" key={i}>
          <input
            aria-label={`${label} ${i + 1}`}
            type={inputType}
            value={v}
            onChange={(e) => update(i, e.target.value)}
          />
          <button
            type="button"
            className="btn-icon btn-remove"
            onClick={() => remove(i)}
            aria-label={`remove ${label} ${i + 1}`}
          >
            ✕
          </button>
        </div>
      ))}
      {canAdd && (
        <button
          type="button"
          className="btn-icon btn-add"
          onClick={add}
          aria-label={`add ${label}`}
        >
          +
        </button>
      )}
    </div>
  );
}

function AddressField({
  label,
  addresses,
  onChange,
  maxItems = 10,
}: {
  label: string;
  addresses: Address[];
  onChange: (addresses: Address[]) => void;
  maxItems?: number;
}) {
  const update = (i: number, patch: Partial<Address>) =>
    onChange(addresses.map((a, j) => (j === i ? { ...a, ...patch } : a)));
  const remove = (i: number) => onChange(addresses.filter((_, j) => j !== i));
  const add = () =>
    onChange([
      ...addresses,
      { type: 'home', street: '', city: '', state: '', postal_code: '', country: '' },
    ]);
  const canAdd = addresses.length < maxItems;

  return (
    <fieldset className="array-field-set">
      <legend>{label}</legend>
      {addresses.map((addr, i) => (
        <div className="address-row" key={i}>
          <div className="address-row-group">
            <div>
              <label htmlFor={`address-type-${i}`}>Type *</label>
              <select
                id={`address-type-${i}`}
                value={addr.type}
                onChange={(e) => update(i, { type: e.target.value as AddressType })}
              >
                <option value="home">Home</option>
                <option value="work">Work</option>
                <option value="other">Other</option>
              </select>
            </div>
            <div>
              <label htmlFor={`address-street-${i}`}>Street *</label>
              <input
                id={`address-street-${i}`}
                type="text"
                placeholder="Street address"
                value={addr.street}
                onChange={(e) => update(i, { street: e.target.value })}
              />
            </div>
            <div>
              <label htmlFor={`address-city-${i}`}>City *</label>
              <input
                id={`address-city-${i}`}
                type="text"
                placeholder="City"
                value={addr.city}
                onChange={(e) => update(i, { city: e.target.value })}
              />
            </div>
          </div>
          <div className="address-row-group">
            <div>
              <label htmlFor={`address-state-${i}`}>State</label>
              <input
                id={`address-state-${i}`}
                type="text"
                placeholder="State"
                value={addr.state || ''}
                onChange={(e) => update(i, { state: e.target.value })}
              />
            </div>
            <div>
              <label htmlFor={`address-postal-${i}`}>Postal Code</label>
              <input
                id={`address-postal-${i}`}
                type="text"
                placeholder="Postal code"
                value={addr.postal_code || ''}
                onChange={(e) => update(i, { postal_code: e.target.value })}
              />
            </div>
            <div>
              <label htmlFor={`address-country-${i}`}>Country</label>
              <input
                id={`address-country-${i}`}
                type="text"
                placeholder="Country"
                value={addr.country || ''}
                onChange={(e) => update(i, { country: e.target.value })}
              />
            </div>
          </div>
          <div className="address-row-group">
            <div>
              <label htmlFor={`address-label-${i}`}>Label</label>
              <input
                id={`address-label-${i}`}
                type="text"
                placeholder="e.g., Apartment 4B"
                value={addr.label || ''}
                onChange={(e) => update(i, { label: e.target.value })}
              />
            </div>
            <div>
              <button
                type="button"
                className="btn-remove-address"
                onClick={() => remove(i)}
                aria-label={`remove address ${i + 1}`}
              >
                Remove
              </button>
            </div>
          </div>
        </div>
      ))}
      {canAdd && (
        <button type="button" onClick={add} aria-label={`add ${label}`}>
          + Add address
        </button>
      )}
    </fieldset>
  );
}

export function PersonForm({
  initial,
  submitting,
  serverErrors,
  onSubmit,
  onCancel,
}: PersonFormProps) {
  const [firstName, setFirstName] = useState(initial?.first_name ?? '');
  const [lastName, setLastName] = useState(initial?.last_name ?? '');
  const [middleNames, setMiddleNames] = useState<string[]>(initial?.middle_names ?? []);
  const [nickname, setNickname] = useState(initial?.nickname ?? '');
  const [pronouns, setPronouns] = useState(initial?.pronouns ?? '');
  const [birthdate, setBirthdate] = useState(initial?.birthdate ?? '');
  const [phoneNumbers, setPhoneNumbers] = useState<string[]>(initial?.phone_numbers ?? []);
  const [addresses, setAddresses] = useState<Address[]>(initial?.addresses ?? []);
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
      middle_names: middleNames.map((s) => s.trim()).filter(Boolean),
      phone_numbers: phoneNumbers.map((s) => s.trim()).filter(Boolean),
      addresses: addresses
        .filter((a) => a.street.trim() || a.city.trim())
        .map((a) => ({
          ...a,
          street: a.street.trim(),
          city: a.city.trim(),
          state: a.state?.trim() || undefined,
          postal_code: a.postal_code?.trim() || undefined,
          country: a.country?.trim() || undefined,
          label: a.label?.trim() || undefined,
        })),
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

      <StringListField
        label="Middle names"
        values={middleNames}
        onChange={setMiddleNames}
        maxItems={16}
      />

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

      <StringListField
        label="Phone numbers"
        values={phoneNumbers}
        onChange={setPhoneNumbers}
        maxItems={10}
      />

      <AddressField label="Addresses" addresses={addresses} onChange={setAddresses} maxItems={10} />

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
