import type { Person } from '../types';

interface PersonListProps {
  persons: Person[];
  onEdit: (person: Person) => void;
  onDelete: (person: Person) => void;
  deleted?: boolean;
  onRestore?: (person: Person) => void;
  onPermanentDelete?: (person: Person) => void;
}

const RESERVED_SYNC_FIELDS = new Set([
  'google_resource_name',
  '_google_updated_at',
  'contacts_local_id',
]);

export function PersonList({
  persons,
  onEdit,
  onDelete,
  deleted,
  onRestore,
  onPermanentDelete,
}: PersonListProps) {
  if (persons.length === 0) {
    return (
      <p className="empty">
        {deleted ? 'The recycle bin is empty.' : 'No contacts yet. Add your first one.'}
      </p>
    );
  }

  return (
    <ul className="person-list">
      {persons.map((person) => (
        <li key={person.id} className="person-item">
          <div className="person-summary">
            <span className="person-name">{person.display_name}</span>
            {(person.phone_numbers ?? []).length > 0 && (
              <span className="person-meta">
                {person.phone_numbers.map((p) => p.value).join(' · ')}
              </span>
            )}
            {Object.entries(person.custom_fields ?? {}).some(
              ([key]) => !RESERVED_SYNC_FIELDS.has(key),
            ) && (
              <span className="person-meta">
                {Object.entries(person.custom_fields)
                  .filter(([key]) => !RESERVED_SYNC_FIELDS.has(key))
                  .map(([k, v]) => `${k}: ${String(v)}`)
                  .join(' · ')}
              </span>
            )}
          </div>
          <div className="person-actions">
            {deleted ? (
              <>
                <button type="button" onClick={() => onRestore?.(person)}>
                  Restore
                </button>
                <button
                  type="button"
                  className="danger"
                  onClick={() => onPermanentDelete?.(person)}
                >
                  Permanently delete
                </button>
              </>
            ) : (
              <>
                <button type="button" onClick={() => onEdit(person)}>
                  Edit
                </button>
                <button type="button" className="danger" onClick={() => onDelete(person)}>
                  Delete
                </button>
              </>
            )}
          </div>
        </li>
      ))}
    </ul>
  );
}
