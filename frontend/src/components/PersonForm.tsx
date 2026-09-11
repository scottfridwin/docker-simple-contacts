import { useState } from 'react';
import type { Address, CustomFieldType, LabeledValue, Person, SyncMetadata } from '../types';
import {
  buildCustomFields,
  inferType,
  MAX_CUSTOM_FIELDS,
  type DraftCustomField,
} from '../customFields';
import { RelationshipsEditor } from './RelationshipsEditor';
import { SharesEditor } from './SharesEditor';

export interface PersonFormValues {
  first_name: string;
  middle_names: string[];
  last_name: string;
  nickname: string;
  pronouns: string;
  birthdate: string;
  emails: LabeledValue[];
  phone_numbers: LabeledValue[];
  addresses: Address[];
  organization: { name: string; title: string; department: string };
  notes: string;
  custom_fields: Record<string, string | number | boolean>;
  labels: string[];
  sync_metadata?: SyncMetadata;
}

interface PersonFormProps {
  initial?: Person;
  submitting?: boolean;
  serverErrors?: Record<string, string>;
  onSubmit: (values: PersonFormValues) => void;
  onCancel: () => void;
  onNavigateToPerson?: (personId: string) => void;
  onLeaveShare?: (person: Person) => void;
}

const RESERVED_SYNC_FIELDS = new Set([
  'google_resource_name',
  '_google_updated_at',
  'contacts_local_id',
]);

function draftsFromPerson(person?: Person): DraftCustomField[] {
  if (!person) return [];
  return Object.entries(person.custom_fields ?? {})
    .filter(([key]) => !RESERVED_SYNC_FIELDS.has(key))
    .map(([key, value]) => ({
      key,
      type: inferType(value),
      value: String(value),
    }));
}

function syncMetadataFromPerson(person?: Person): SyncMetadata {
  if (!person) return {};
  return {
    googleResourceName: (person.custom_fields?.google_resource_name as string | undefined) ?? null,
    googleUpdatedAt: (person.custom_fields?._google_updated_at as string | undefined) ?? null,
  };
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

function LabeledListField({
  label,
  values,
  onChange,
  maxItems,
  inputType = 'text',
}: {
  label: string;
  values: LabeledValue[];
  onChange: (values: LabeledValue[]) => void;
  maxItems?: number;
  inputType?: string;
}) {
  const update = (i: number, patch: Partial<LabeledValue>) =>
    onChange(values.map((v, j) => (j === i ? { ...v, ...patch } : v)));
  const remove = (i: number) => onChange(values.filter((_, j) => j !== i));
  const add = () => onChange([...values, { label: '', value: '' }]);
  const canAdd = maxItems === undefined || values.length < maxItems;

  return (
    <div className="array-field">
      <span className="array-field-label">{label}</span>
      {values.map((v, i) => (
        <div className="array-field-row" key={i}>
          <input
            aria-label={`${label} ${i + 1} label`}
            placeholder="label"
            className="labeled-field-label"
            value={v.label}
            onChange={(e) => update(i, { label: e.target.value })}
          />
          <input
            aria-label={`${label} ${i + 1}`}
            type={inputType}
            value={v.value}
            onChange={(e) => update(i, { value: e.target.value })}
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

function AddressListField({
  values,
  onChange,
  maxItems,
}: {
  values: Address[];
  onChange: (values: Address[]) => void;
  maxItems?: number;
}) {
  const update = (i: number, patch: Partial<Address>) =>
    onChange(values.map((v, j) => (j === i ? { ...v, ...patch } : v)));
  const remove = (i: number) => onChange(values.filter((_, j) => j !== i));
  const add = () =>
    onChange([
      ...values,
      { label: '', street: '', city: '', region: '', postal_code: '', country: '' },
    ]);
  const canAdd = maxItems === undefined || values.length < maxItems;

  return (
    <div className="array-field">
      <span className="array-field-label">Addresses</span>
      {values.map((v, i) => (
        <div className="address-field-row" key={i}>
          <input
            aria-label={`address ${i + 1} label`}
            placeholder="label"
            value={v.label ?? ''}
            onChange={(e) => update(i, { label: e.target.value })}
          />
          <input
            aria-label={`address ${i + 1} street`}
            placeholder="street"
            value={v.street ?? ''}
            onChange={(e) => update(i, { street: e.target.value })}
          />
          <input
            aria-label={`address ${i + 1} city`}
            placeholder="city"
            value={v.city ?? ''}
            onChange={(e) => update(i, { city: e.target.value })}
          />
          <input
            aria-label={`address ${i + 1} region`}
            placeholder="region"
            value={v.region ?? ''}
            onChange={(e) => update(i, { region: e.target.value })}
          />
          <input
            aria-label={`address ${i + 1} postal code`}
            placeholder="postal code"
            value={v.postal_code ?? ''}
            onChange={(e) => update(i, { postal_code: e.target.value })}
          />
          <input
            aria-label={`address ${i + 1} country`}
            placeholder="country"
            value={v.country ?? ''}
            onChange={(e) => update(i, { country: e.target.value })}
          />
          <button
            type="button"
            className="btn-icon btn-remove"
            onClick={() => remove(i)}
            aria-label={`remove address ${i + 1}`}
          >
            ✕
          </button>
        </div>
      ))}
      {canAdd && (
        <button type="button" className="btn-icon btn-add" onClick={add} aria-label="add address">
          +
        </button>
      )}
    </div>
  );
}

export function PersonForm({
  initial,
  submitting,
  serverErrors,
  onSubmit,
  onCancel,
  onNavigateToPerson,
  onLeaveShare,
}: PersonFormProps) {
  const [firstName, setFirstName] = useState(initial?.first_name ?? '');
  const [lastName, setLastName] = useState(initial?.last_name ?? '');
  const [middleNames, setMiddleNames] = useState<string[]>(initial?.middle_names ?? []);
  const [nickname, setNickname] = useState(initial?.nickname ?? '');
  const [pronouns, setPronouns] = useState(initial?.pronouns ?? '');
  const [birthdate, setBirthdate] = useState(initial?.birthdate ?? '');
  const [emails, setEmails] = useState<LabeledValue[]>(initial?.emails ?? []);
  const [phoneNumbers, setPhoneNumbers] = useState<LabeledValue[]>(initial?.phone_numbers ?? []);
  const [addresses, setAddresses] = useState<Address[]>(initial?.addresses ?? []);
  const [orgName, setOrgName] = useState(initial?.organization?.name ?? '');
  const [orgTitle, setOrgTitle] = useState(initial?.organization?.title ?? '');
  const [orgDepartment, setOrgDepartment] = useState(initial?.organization?.department ?? '');
  const [notes, setNotes] = useState(initial?.notes ?? '');
  const [labels, setLabels] = useState<string[]>(initial?.labels ?? []);
  const [drafts, setDrafts] = useState<DraftCustomField[]>(draftsFromPerson(initial));
  const [syncMetadata] = useState<SyncMetadata>(syncMetadataFromPerson(initial));
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
      emails: emails
        .map((e) => ({ label: e.label.trim(), value: e.value.trim() }))
        .filter((e) => e.value),
      phone_numbers: phoneNumbers
        .map((p) => ({ label: p.label.trim(), value: p.value.trim() }))
        .filter((p) => p.value),
      addresses: addresses
        .map((a) => ({
          label: a.label?.trim() ?? '',
          street: a.street?.trim() ?? '',
          city: a.city?.trim() ?? '',
          region: a.region?.trim() ?? '',
          postal_code: a.postal_code?.trim() ?? '',
          country: a.country?.trim() ?? '',
        }))
        .filter((a) => a.street || a.city || a.region || a.postal_code || a.country),
      organization: {
        name: orgName.trim(),
        title: orgTitle.trim(),
        department: orgDepartment.trim(),
      },
      notes: notes.trim(),
      labels: labels.map((l) => l.trim()).filter(Boolean),
      custom_fields: {
        ...fields,
        ...(syncMetadata.googleResourceName
          ? { google_resource_name: syncMetadata.googleResourceName }
          : {}),
        ...(syncMetadata.googleUpdatedAt
          ? { _google_updated_at: syncMetadata.googleUpdatedAt }
          : {}),
      },
      sync_metadata: syncMetadata,
    });
  };

  const combinedErrors = { ...errors, ...serverErrors };

  return (
    <form onSubmit={handleSubmit} className="person-form" aria-label="person form">
      {initial && initial.is_owner === false && (
        <p className="shared-by-banner">
          Shared by {initial.owner_display_name ?? 'another account'}
          {onLeaveShare && (
            <button type="button" className="link-button" onClick={() => onLeaveShare(initial)}>
              Remove from my contacts
            </button>
          )}
        </p>
      )}

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

      <LabeledListField label="Emails" values={emails} onChange={setEmails} maxItems={10} />

      <LabeledListField
        label="Phone numbers"
        values={phoneNumbers}
        onChange={setPhoneNumbers}
        maxItems={10}
      />

      <AddressListField values={addresses} onChange={setAddresses} maxItems={10} />

      {initial && initial.is_owner !== false && (
        <>
          <RelationshipsEditor personId={initial.id} onNavigateToPerson={onNavigateToPerson} />
          <SharesEditor personId={initial.id} />
        </>
      )}

      <fieldset className="organization-field">
        <legend>Organization</legend>
        <div className="field">
          <label htmlFor="org_name">Company</label>
          <input id="org_name" value={orgName} onChange={(e) => setOrgName(e.target.value)} />
        </div>
        <div className="field">
          <label htmlFor="org_title">Title</label>
          <input id="org_title" value={orgTitle} onChange={(e) => setOrgTitle(e.target.value)} />
        </div>
        <div className="field">
          <label htmlFor="org_department">Department</label>
          <input
            id="org_department"
            value={orgDepartment}
            onChange={(e) => setOrgDepartment(e.target.value)}
          />
        </div>
      </fieldset>

      <div className="field">
        <label htmlFor="notes">Notes</label>
        <textarea id="notes" value={notes} onChange={(e) => setNotes(e.target.value)} rows={4} />
      </div>

      <StringListField label="Labels" values={labels} onChange={setLabels} maxItems={25} />

      <fieldset className="custom-fields">
        <legend>Custom fields</legend>
        {drafts.length > 0 && (
          <div className="custom-field-row custom-field-headers" aria-hidden="true">
            <span>Key</span>
            <span>Type</span>
            <span>Value</span>
            <span />
          </div>
        )}
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
                placeholder="value"
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

      {(syncMetadata.googleResourceName || syncMetadata.googleUpdatedAt) && (
        <fieldset className="sync-metadata" aria-label="sync metadata">
          <legend>Sync metadata</legend>
          <div className="sync-metadata-grid">
            {syncMetadata.googleResourceName && (
              <div>
                <span className="sync-metadata-label">Google resource</span>
                <code className="sync-metadata-value">{syncMetadata.googleResourceName}</code>
              </div>
            )}
            {syncMetadata.googleUpdatedAt && (
              <div>
                <span className="sync-metadata-label">Google updated</span>
                <code className="sync-metadata-value">{syncMetadata.googleUpdatedAt}</code>
              </div>
            )}
          </div>
        </fieldset>
      )}

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
