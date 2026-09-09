import { useEffect, useState } from 'react';
import { createRelationship, deleteRelationship, listPersons, listRelationships } from '../api';
import { RELATION_TYPES, type Person, type RelationType, type Relationship } from '../types';

interface RelationshipsEditorProps {
  personId: string;
  onNavigateToPerson?: (personId: string) => void;
}

const RELATION_LABELS: Record<RelationType, string> = {
  parent: 'Parent',
  child: 'Child',
  spouse: 'Spouse',
  sibling: 'Sibling',
  partner: 'Partner',
};

export function RelationshipsEditor({ personId, onNavigateToPerson }: RelationshipsEditorProps) {
  const [relationships, setRelationships] = useState<Relationship[]>([]);
  const [candidates, setCandidates] = useState<Person[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [type, setType] = useState<RelationType>('parent');
  const [mode, setMode] = useState<'link' | 'name'>('link');
  const [relatedPersonId, setRelatedPersonId] = useState('');
  const [relatedName, setRelatedName] = useState('');
  const [submitting, setSubmitting] = useState(false);

  const refresh = async () => {
    setLoading(true);
    setError(null);
    try {
      const [relRes, personsRes] = await Promise.all([
        listRelationships(personId),
        listPersons({ pageSize: 100, sort: 'last_name', order: 'asc' }),
      ]);
      setRelationships(relRes.data);
      setCandidates(personsRes.data.filter((p) => p.id !== personId));
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load relationships');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    queueMicrotask(() => {
      void refresh();
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [personId]);

  const handleAdd = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    if (mode === 'link' && !relatedPersonId) {
      setError('Choose a contact to link, or switch to entering a name.');
      return;
    }
    if (mode === 'name' && !relatedName.trim()) {
      setError('Enter a name.');
      return;
    }
    setSubmitting(true);
    try {
      await createRelationship(personId, {
        type,
        related_person_id: mode === 'link' ? relatedPersonId : undefined,
        related_person_name: mode === 'name' ? relatedName.trim() : undefined,
      });
      setRelatedPersonId('');
      setRelatedName('');
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to add relationship');
    } finally {
      setSubmitting(false);
    }
  };

  const handleRemove = async (relationship: Relationship) => {
    setError(null);
    try {
      await deleteRelationship(personId, relationship.id);
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to remove relationship');
    }
  };

  return (
    <fieldset className="relationships-editor" aria-label="relationships">
      <legend>Relationships</legend>
      {error && <span className="error">{error}</span>}
      {loading ? (
        <p>Loading relationships…</p>
      ) : relationships.length === 0 ? (
        <p className="empty">No relationships yet.</p>
      ) : (
        <ul className="relationship-list">
          {relationships.map((rel) => (
            <li key={rel.id} className="relationship-item">
              <span className="relationship-type">{RELATION_LABELS[rel.type]}</span>
              {rel.related_person_id ? (
                <button
                  type="button"
                  className="link-button"
                  onClick={() => onNavigateToPerson?.(rel.related_person_id!)}
                >
                  {rel.related_person_name}
                  {rel.related_person_deleted ? ' (deleted)' : ''}
                </button>
              ) : (
                <span>{rel.related_person_name}</span>
              )}
              <button
                type="button"
                className="btn-icon btn-remove"
                onClick={() => handleRemove(rel)}
                aria-label={`remove relationship to ${rel.related_person_name}`}
              >
                ✕
              </button>
            </li>
          ))}
        </ul>
      )}

      <form className="relationship-add-form" onSubmit={handleAdd}>
        <select
          aria-label="relationship type"
          value={type}
          onChange={(e) => setType(e.target.value as RelationType)}
        >
          {RELATION_TYPES.map((t) => (
            <option key={t} value={t}>
              {RELATION_LABELS[t]}
            </option>
          ))}
        </select>
        <div className="relationship-mode-toggle">
          <label>
            <input type="radio" checked={mode === 'link'} onChange={() => setMode('link')} />
            Existing contact
          </label>
          <label>
            <input type="radio" checked={mode === 'name'} onChange={() => setMode('name')} />
            Just a name
          </label>
        </div>
        {mode === 'link' ? (
          <select
            aria-label="related contact"
            value={relatedPersonId}
            onChange={(e) => setRelatedPersonId(e.target.value)}
          >
            <option value="">Select a contact…</option>
            {candidates.map((p) => (
              <option key={p.id} value={p.id}>
                {p.display_name}
              </option>
            ))}
          </select>
        ) : (
          <input
            aria-label="related person name"
            placeholder="Name"
            value={relatedName}
            onChange={(e) => setRelatedName(e.target.value)}
          />
        )}
        <button type="submit" disabled={submitting}>
          {submitting ? 'Adding…' : 'Add relationship'}
        </button>
      </form>
    </fieldset>
  );
}
