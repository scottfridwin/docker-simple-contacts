import { useCallback, useEffect, useState } from 'react';
import { ApiRequestError, createPerson, deletePerson, listPersons, updatePerson } from './api';
import type { Person } from './types';
import { PersonForm, type PersonFormValues } from './components/PersonForm';
import { PersonList } from './components/PersonList';

type View = { mode: 'list' } | { mode: 'create' } | { mode: 'edit'; person: Person };

export default function App() {
  const [persons, setPersons] = useState<Person[]>([]);
  const [view, setView] = useState<View>({ mode: 'list' });
  const [loading, setLoading] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [serverErrors, setServerErrors] = useState<Record<string, string>>({});

  const refresh = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const res = await listPersons({ sort: 'display_name', order: 'desc' });
      setPersons(res.data);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load contacts');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  const handleSubmit = async (values: PersonFormValues) => {
    setSubmitting(true);
    setServerErrors({});
    setError(null);
    try {
      const payload = {
        first_name: values.first_name,
        last_name: values.last_name,
        middle_names: values.middle_names,
        nickname: values.nickname || undefined,
        pronouns: values.pronouns || undefined,
        birthdate: values.birthdate || undefined,
        phone_numbers: values.phone_numbers.length ? values.phone_numbers : undefined,
        custom_fields: values.custom_fields,
      };
      if (view.mode === 'edit') {
        await updatePerson(view.person.id, payload);
      } else {
        await createPerson(payload);
      }
      setView({ mode: 'list' });
      await refresh();
    } catch (err) {
      if (err instanceof ApiRequestError && err.details) {
        const mapped: Record<string, string> = {};
        for (const d of err.details) {
          mapped[d.field] = d.message;
        }
        setServerErrors(mapped);
      } else {
        setError(err instanceof Error ? err.message : 'Save failed');
      }
    } finally {
      setSubmitting(false);
    }
  };

  const handleDelete = async (person: Person) => {
    if (!window.confirm(`Delete ${person.display_name}? It can be recovered for 30 days.`)) {
      return;
    }
    setError(null);
    try {
      await deletePerson(person.id);
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Delete failed');
    }
  };

  return (
    <main className="app">
      <header className="app-header">
        <h1>Contacts</h1>
        {view.mode === 'list' && (
          <button type="button" onClick={() => setView({ mode: 'create' })}>
            Add contact
          </button>
        )}
      </header>

      {error && (
        <div className="banner error" role="alert">
          {error}
        </div>
      )}

      {view.mode === 'list' && (
        <>
          {loading ? (
            <p>Loading…</p>
          ) : (
            <PersonList
              persons={persons}
              onEdit={(person) => setView({ mode: 'edit', person })}
              onDelete={handleDelete}
            />
          )}
        </>
      )}

      {view.mode !== 'list' && (
        <section className="editor">
          <h2>{view.mode === 'edit' ? 'Edit contact' : 'New contact'}</h2>
          <PersonForm
            initial={view.mode === 'edit' ? view.person : undefined}
            submitting={submitting}
            serverErrors={serverErrors}
            onSubmit={handleSubmit}
            onCancel={() => setView({ mode: 'list' })}
          />
        </section>
      )}
    </main>
  );
}
