import { useCallback, useEffect, useState } from 'react';
import {
  ApiRequestError,
  createPerson,
  deletePerson,
  listDeletedPersons,
  listPersons,
  permanentlyDeletePerson,
  restorePerson,
  updatePerson,
} from './api';
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
  const [search, setSearch] = useState({ firstName: '', lastName: '' });
  const [page, setPage] = useState(1);
  const [totalPages, setTotalPages] = useState(1);
  const [showDeleted, setShowDeleted] = useState(false);

  const refresh = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const loader = showDeleted ? listDeletedPersons : listPersons;
      const res = await loader({
        page,
        sort: 'display_name',
        order: 'desc',
        firstName: search.firstName || undefined,
        lastName: search.lastName || undefined,
      });
      setPersons(res.data);
      setTotalPages(res.total_pages);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load contacts');
    } finally {
      setLoading(false);
    }
  }, [page, search, showDeleted]);

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

  const handleRestore = async (person: Person) => {
    setError(null);
    try {
      await restorePerson(person.id);
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Restore failed');
    }
  };

  const handlePermanentDelete = async (person: Person) => {
    if (!window.confirm(`Permanently delete ${person.display_name}? This cannot be undone.`))
      return;
    setError(null);
    try {
      await permanentlyDeletePerson(person.id);
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Permanent delete failed');
    }
  };

  return (
    <main className="app">
      <header className="app-header">
        <h1>Contacts</h1>
        {view.mode === 'list' && !showDeleted && (
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
          <div className="list-controls">
            {!showDeleted && (
              <>
                <input
                  aria-label="filter first name"
                  placeholder="First name"
                  value={search.firstName}
                  onChange={(e) => {
                    setPage(1);
                    setSearch((s) => ({ ...s, firstName: e.target.value }));
                  }}
                />
                <input
                  aria-label="filter last name"
                  placeholder="Last name"
                  value={search.lastName}
                  onChange={(e) => {
                    setPage(1);
                    setSearch((s) => ({ ...s, lastName: e.target.value }));
                  }}
                />
              </>
            )}
            <button
              type="button"
              onClick={() => {
                setPage(1);
                setSearch({ firstName: '', lastName: '' });
                setShowDeleted((v) => !v);
              }}
            >
              {showDeleted ? 'Active contacts' : 'Recycle bin'}
            </button>
          </div>
          {loading ? (
            <p>Loading…</p>
          ) : (
            <PersonList
              persons={persons}
              onEdit={(person) => setView({ mode: 'edit', person })}
              onDelete={handleDelete}
              deleted={showDeleted}
              onRestore={handleRestore}
              onPermanentDelete={handlePermanentDelete}
            />
          )}
          {totalPages > 1 && (
            <nav aria-label="pagination">
              <button type="button" disabled={page === 1} onClick={() => setPage((p) => p - 1)}>
                Previous
              </button>
              <span>
                Page {page} of {totalPages}
              </span>
              <button
                type="button"
                disabled={page >= totalPages}
                onClick={() => setPage((p) => p + 1)}
              >
                Next
              </button>
            </nav>
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
