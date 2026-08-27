import { useCallback, useEffect, useState } from 'react';
import type { Person, Relationship } from '../types';
import { getRelationshipLabel } from '../utils/relationships';
import * as api from '../api';

interface PersonDetailProps {
  person: Person;
  onBack: () => void;
  onEdit: (person: Person) => void;
  onDelete: (person: Person) => void;
}

export function PersonDetail({ person, onBack, onEdit, onDelete }: PersonDetailProps) {
  const [relationships, setRelationships] = useState<Relationship[]>([]);
  const [relationshipPersons, setRelationshipPersons] = useState<Record<string, Person>>({});
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const loadRelationships = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const rels = await api.listRelationshipsForPerson(person.id);
      setRelationships(rels);

      // Load the related persons
      const otherIds = new Set<string>();
      rels.forEach((rel) => {
        if (rel.person_id_1 === person.id) {
          otherIds.add(rel.person_id_2);
        } else {
          otherIds.add(rel.person_id_1);
        }
      });

      const persons: Record<string, Person> = {};
      await Promise.all(
        Array.from(otherIds).map(async (id) => {
          try {
            persons[id] = await api.getPerson(id);
          } catch (e) {
            console.error(`Failed to load person ${id}:`, e);
          }
        }),
      );
      setRelationshipPersons(persons);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load relationships');
    } finally {
      setLoading(false);
    }
  }, [person.id]);

  useEffect(() => {
    void loadRelationships();
  }, [loadRelationships]);

  const handleDeleteRelationship = async (rel: Relationship) => {
    if (!window.confirm(`Delete this relationship?`)) {
      return;
    }
    try {
      await api.deleteRelationship(rel.id);
      await loadRelationships();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to delete relationship');
    }
  };

  return (
    <div className="person-detail">
      <button type="button" onClick={onBack} className="btn-back">
        ← Back
      </button>

      <div className="person-detail-header">
        <div>
          <h2>{person.display_name}</h2>
          {person.nickname && <p className="muted">Nickname: {person.nickname}</p>}
        </div>
        <div className="person-detail-actions">
          <button type="button" onClick={() => onEdit(person)}>
            Edit
          </button>
          <button
            type="button"
            className="danger"
            onClick={() => onDelete(person)}
          >
            Delete
          </button>
        </div>
      </div>

      <div className="person-detail-body">
        {person.pronouns && (
          <div className="detail-section">
            <strong>Pronouns:</strong> {person.pronouns}
          </div>
        )}

        {person.birthdate && (
          <div className="detail-section">
            <strong>Birthdate:</strong> {person.birthdate}
          </div>
        )}

        {(person.phone_numbers ?? []).length > 0 && (
          <div className="detail-section">
            <strong>Phone:</strong>
            <ul>
              {person.phone_numbers.map((num, i) => (
                <li key={i}>{num}</li>
              ))}
            </ul>
          </div>
        )}

        {(person.addresses ?? []).length > 0 && (
          <div className="detail-section">
            <strong>Addresses:</strong>
            <ul>
              {person.addresses.map((addr, i) => (
                <li key={i}>
                  <strong>{addr.label || addr.type}:</strong> {addr.street}, {addr.city}
                  {addr.state && `, ${addr.state}`}
                  {addr.postal_code && ` ${addr.postal_code}`}
                  {addr.country && `, ${addr.country}`}
                </li>
              ))}
            </ul>
          </div>
        )}

        {Object.keys(person.custom_fields ?? {}).length > 0 && (
          <div className="detail-section">
            <strong>Custom Fields:</strong>
            <ul>
              {Object.entries(person.custom_fields).map(([key, value]) => (
                <li key={key}>
                  <strong>{key}:</strong> {String(value)}
                </li>
              ))}
            </ul>
          </div>
        )}

        <div className="detail-section">
          <h3>Relationships</h3>
          {loading && <p>Loading relationships...</p>}
          {error && <p className="error">{error}</p>}
          {relationships.length === 0 && !loading && (
            <p className="muted">No relationships yet.</p>
          )}
          {relationships.length > 0 && (
            <ul className="relationships-list">
              {relationships.map((rel) => {
                const otherId =
                  rel.person_id_1 === person.id ? rel.person_id_2 : rel.person_id_1;
                const otherPerson = relationshipPersons[otherId];
                if (!otherPerson) return null;

                return (
                  <li key={rel.id} className="relationship-item">
                    <div className="relationship-info">
                      <strong>{getRelationshipLabel(rel, person.id, otherPerson)}</strong>
                      <span className="muted">
                        {' '}
                        · {otherPerson.display_name}
                      </span>
                      {rel.label && <span className="muted"> · {rel.label}</span>}
                    </div>
                    <button
                      type="button"
                      className="danger"
                      onClick={() => handleDeleteRelationship(rel)}
                    >
                      Remove
                    </button>
                  </li>
                );
              })}
            </ul>
          )}
        </div>
      </div>
    </div>
  );
}
