import type { Person } from '../types';

interface PersonListProps {
  persons: Person[];
  onEdit: (person: Person) => void;
  onDelete: (person: Person) => void;
}

export function PersonList({ persons, onEdit, onDelete }: PersonListProps) {
  if (persons.length === 0) {
    return <p className="empty">No contacts yet. Add your first one.</p>;
  }

  return (
    <ul className="person-list">
      {persons.map((person) => (
        <li key={person.id} className="person-item">
          <div className="person-summary">
            <span className="person-name">{person.display_name}</span>
            {(person.phone_numbers ?? []).length > 0 && (
              <span className="person-meta">{person.phone_numbers.join(' · ')}</span>
            )}
            {Object.keys(person.custom_fields ?? {}).length > 0 && (
              <span className="person-meta">
                {Object.entries(person.custom_fields)
                  .map(([k, v]) => `${k}: ${String(v)}`)
                  .join(' · ')}
              </span>
            )}
          </div>
          <div className="person-actions">
            <button type="button" onClick={() => onEdit(person)}>
              Edit
            </button>
            <button type="button" className="danger" onClick={() => onDelete(person)}>
              Delete
            </button>
          </div>
        </li>
      ))}
    </ul>
  );
}
