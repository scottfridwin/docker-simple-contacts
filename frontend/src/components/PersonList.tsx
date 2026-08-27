import type { Person } from '../types';

interface PersonListProps {
  persons: Person[];
  onView: (person: Person) => void;
  onEdit: (person: Person) => void;
  onDelete: (person: Person) => void;
}

export function PersonList({ persons, onView, onEdit, onDelete }: PersonListProps) {
  if (persons.length === 0) {
    return <p className="empty">No contacts yet. Add your first one.</p>;
  }

  return (
    <ul className="person-list">
      {persons.map((person) => (
        <li key={person.id} className="person-item">
          <div className="person-summary">
            <button
              type="button"
              className="person-name-button"
              onClick={() => onView(person)}
            >
              {person.display_name}
            </button>
            {(person.phone_numbers ?? []).length > 0 && (
              <span className="person-meta">{person.phone_numbers.join(' · ')}</span>
            )}
            {(person.addresses ?? []).length > 0 && (
              <span className="person-meta">
                {person.addresses
                  .map(
                    (a) =>
                      `${a.label || a.type}: ${a.street}, ${a.city}${a.state ? ', ' + a.state : ''}`
                  )
                  .join(' · ')}
              </span>
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
