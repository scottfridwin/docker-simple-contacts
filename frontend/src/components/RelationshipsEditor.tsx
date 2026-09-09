import { useEffect, useRef, useState } from 'react';
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

const SEARCH_DEBOUNCE_MS = 250;
const MAX_SUGGESTIONS = 8;

/** Looks up contacts matching query by first OR last name, merging and capping results client-side. */
async function searchContacts(query: string, excludePersonId: string): Promise<Person[]> {
  const trimmed = query.trim();
  if (!trimmed) return [];
  const [byFirst, byLast] = await Promise.all([
    listPersons({ firstName: trimmed, pageSize: MAX_SUGGESTIONS, sort: 'last_name', order: 'asc' }),
    listPersons({ lastName: trimmed, pageSize: MAX_SUGGESTIONS, sort: 'last_name', order: 'asc' }),
  ]);
  const merged = new Map<string, Person>();
  for (const p of [...byFirst.data, ...byLast.data]) {
    if (p.id !== excludePersonId) merged.set(p.id, p);
  }
  return Array.from(merged.values()).slice(0, MAX_SUGGESTIONS);
}

/** A text input that suggests matching contacts (by first or last name) as the user types. */
function ContactCombobox({
  personId,
  query,
  selected,
  onQueryChange,
  onSelect,
}: {
  personId: string;
  query: string;
  selected: Person | null;
  onQueryChange: (value: string) => void;
  onSelect: (person: Person) => void;
}) {
  const [suggestions, setSuggestions] = useState<Person[]>([]);
  const [open, setOpen] = useState(false);
  const [highlighted, setHighlighted] = useState(0);
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    return () => {
      if (debounceRef.current) clearTimeout(debounceRef.current);
    };
  }, []);

  const runSearch = (value: string) => {
    if (debounceRef.current) clearTimeout(debounceRef.current);
    if (!value.trim()) {
      setSuggestions([]);
      setOpen(false);
      return;
    }
    debounceRef.current = setTimeout(() => {
      void searchContacts(value, personId).then((results) => {
        setSuggestions(results);
        setOpen(results.length > 0);
        setHighlighted(0);
      });
    }, SEARCH_DEBOUNCE_MS);
  };

  const handleChange = (value: string) => {
    onQueryChange(value);
    runSearch(value);
  };

  const handleSelect = (person: Person) => {
    onSelect(person);
    setOpen(false);
  };

  const handleKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (!open || suggestions.length === 0) return;
    if (e.key === 'ArrowDown') {
      e.preventDefault();
      setHighlighted((i) => (i + 1) % suggestions.length);
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      setHighlighted((i) => (i - 1 + suggestions.length) % suggestions.length);
    } else if (e.key === 'Enter') {
      e.preventDefault();
      handleSelect(suggestions[highlighted]);
    } else if (e.key === 'Escape') {
      setOpen(false);
    }
  };

  return (
    <div className="contact-combobox">
      <input
        aria-label="related contact"
        role="combobox"
        aria-expanded={open}
        aria-autocomplete="list"
        placeholder="Type a name…"
        value={query}
        onChange={(e) => handleChange(e.target.value)}
        onFocus={() => setOpen(suggestions.length > 0)}
        onBlur={() => setTimeout(() => setOpen(false), 150)}
        onKeyDown={handleKeyDown}
      />
      {selected && (
        <span className="combobox-linked-hint" aria-hidden="true">
          ✓ linked
        </span>
      )}
      {open && (
        <ul className="combobox-suggestions" role="listbox">
          {suggestions.map((p, i) => (
            <li key={p.id} role="option" aria-selected={i === highlighted}>
              <button
                type="button"
                className={i === highlighted ? 'is-highlighted' : ''}
                onMouseDown={(e) => e.preventDefault()}
                onClick={() => handleSelect(p)}
              >
                {p.display_name}
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

export function RelationshipsEditor({ personId, onNavigateToPerson }: RelationshipsEditorProps) {
  const [relationships, setRelationships] = useState<Relationship[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [type, setType] = useState<RelationType>('parent');
  const [query, setQuery] = useState('');
  const [selected, setSelected] = useState<Person | null>(null);
  const [submitting, setSubmitting] = useState(false);

  const refresh = async () => {
    setLoading(true);
    setError(null);
    try {
      const relRes = await listRelationships(personId);
      setRelationships(relRes.data);
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

  const handleQueryChange = (value: string) => {
    setQuery(value);
    if (selected && value !== selected.display_name) {
      setSelected(null);
    }
  };

  const handleSelectContact = (person: Person) => {
    setSelected(person);
    setQuery(person.display_name);
  };

  const handleAdd = async () => {
    setError(null);
    const name = query.trim();
    if (!name) {
      setError('Enter a name or choose a contact.');
      return;
    }
    setSubmitting(true);
    try {
      // Link to the selected contact only if the text still matches it
      // exactly; otherwise store whatever was typed as a name-only entry.
      const isLinked = selected !== null && selected.display_name === name;
      await createRelationship(personId, {
        type,
        related_person_id: isLinked ? selected!.id : undefined,
        related_person_name: isLinked ? undefined : name,
      });
      setQuery('');
      setSelected(null);
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
                <span className="relationship-unlinked">
                  {rel.related_person_name}
                  <span className="unlinked-badge">not linked</span>
                </span>
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

      <div className="relationship-add-form">
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
        <ContactCombobox
          personId={personId}
          query={query}
          selected={selected}
          onQueryChange={handleQueryChange}
          onSelect={handleSelectContact}
        />
        <button
          type="button"
          className="btn-icon btn-add"
          onClick={() => void handleAdd()}
          disabled={submitting}
          aria-label="add relationship"
        >
          +
        </button>
      </div>
    </fieldset>
  );
}
