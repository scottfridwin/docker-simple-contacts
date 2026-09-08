import { useCallback, useEffect, useState } from 'react';
import {
  ApiRequestError,
  beginGoogleSync,
  createPerson,
  deletePerson,
  listDeletedPersons,
  listSyncAccounts,
  listPersons,
  permanentlyDeletePerson,
  restorePerson,
  updatePerson,
} from './api';
import type { Person, SyncAccount } from './types';
import { PersonForm, type PersonFormValues } from './components/PersonForm';
import { PersonList } from './components/PersonList';

type View = { mode: 'list' } | { mode: 'create' } | { mode: 'edit'; person: Person };

function getAppPathname() {
  return window.location.pathname.replace(/\/+$/, '') || '/';
}

function PrivacyPolicyPage() {
  return (
    <main className="landing-page privacy-page">
      <section className="landing-card privacy-card" aria-labelledby="privacy-title">
        <p className="landing-eyebrow">PRIVACY POLICY</p>
        <h1 id="privacy-title">Privacy Policy</h1>
        <p className="privacy-updated">Last updated: September 8, 2026</p>
        <div className="privacy-copy">
          <p>
            This service is intended for a small, private family and direct-relations group. It is
            not operated as a public consumer service.
          </p>
          <p>
            We collect and store the contact information you choose to enter into the application,
            including names, phone numbers, custom fields, and sync configuration needed to connect
            Google Contacts.
          </p>
          <p>
            Google account authorization is used only to synchronize contacts that you choose to
            connect. We do not sell personal data, do not use it for advertising, and do not
            intentionally share it with unrelated third parties.
          </p>
          <p>
            Data is stored to provide contact management and synchronization functionality. If you
            delete a contact, it is soft-deleted first and later purged according to the application
            retention policy.
          </p>
          <p>If you have questions about this policy, contact the site operator.</p>
        </div>
        <a className="sso-button privacy-home-link" href="/">
          <span aria-hidden="true">←</span>
          Back to contacts
        </a>
      </section>
    </main>
  );
}

export default function App() {
  const isPrivacyPage = getAppPathname() === '/privacy';
  const [persons, setPersons] = useState<Person[]>([]);
  const [syncAccounts, setSyncAccounts] = useState<SyncAccount[]>([]);
  const [view, setView] = useState<View>({ mode: 'list' });
  const [loading, setLoading] = useState(false);
  const [syncLoading, setSyncLoading] = useState(false);
  const [syncConnecting, setSyncConnecting] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [syncError, setSyncError] = useState<string | null>(null);
  const [authenticationRequired, setAuthenticationRequired] = useState(false);
  const [serverErrors, setServerErrors] = useState<Record<string, string>>({});
  const [search, setSearch] = useState({ firstName: '', lastName: '' });
  const [page, setPage] = useState(1);
  const [totalPages, setTotalPages] = useState(1);
  const [showDeleted, setShowDeleted] = useState(false);

  const refresh = useCallback(async () => {
    if (isPrivacyPage) {
      return;
    }
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
      setAuthenticationRequired(false);
    } catch (err) {
      if (err instanceof ApiRequestError && err.status === 401) {
        setAuthenticationRequired(true);
        setError(null);
      } else {
        setError(err instanceof Error ? err.message : 'Failed to load contacts');
      }
    } finally {
      setLoading(false);
    }
  }, [isPrivacyPage, page, search, showDeleted]);

  const refreshSyncAccounts = useCallback(async () => {
    if (isPrivacyPage) {
      return;
    }
    setSyncLoading(true);
    setSyncError(null);
    try {
      const res = await listSyncAccounts();
      setSyncAccounts(res.data);
    } catch (err) {
      if (err instanceof ApiRequestError && err.status === 401) {
        setSyncAccounts([]);
      } else {
        setSyncError(err instanceof Error ? err.message : 'Failed to load sync accounts');
      }
    } finally {
      setSyncLoading(false);
    }
  }, [isPrivacyPage]);

  useEffect(() => {
    if (isPrivacyPage) {
      return;
    }
    queueMicrotask(() => {
      void refresh();
    });
  }, [isPrivacyPage, refresh]);

  useEffect(() => {
    if (isPrivacyPage) {
      return;
    }
    if (authenticationRequired) {
      return;
    }
    queueMicrotask(() => {
      void refreshSyncAccounts();
    });
  }, [authenticationRequired, isPrivacyPage, refreshSyncAccounts]);

  useEffect(() => {
    if (isPrivacyPage) {
      return;
    }
    const onFocus = () => {
      void refreshSyncAccounts();
    };
    window.addEventListener('focus', onFocus);
    return () => window.removeEventListener('focus', onFocus);
  }, [isPrivacyPage, refreshSyncAccounts]);

  if (isPrivacyPage) {
    return <PrivacyPolicyPage />;
  }

  const handleConnectGoogle = async () => {
    setSyncConnecting(true);
    setSyncError(null);
    try {
      const { authorization_url: authorizationUrl } = await beginGoogleSync(
        `${window.location.origin}/api/v1/sync/google/callback`,
      );
      const popup = window.open(authorizationUrl, 'google-sync', 'popup,width=520,height=760');
      if (!popup) {
        window.location.assign(authorizationUrl);
      }
    } catch (err) {
      setSyncError(err instanceof Error ? err.message : 'Failed to start Google sync');
    } finally {
      setSyncConnecting(false);
    }
  };

  if (authenticationRequired) {
    return (
      <main className="landing-page">
        <section className="landing-card" aria-labelledby="landing-title">
          <div className="landing-banner" aria-hidden="true">
            <span className="landing-mark">FW</span>
          </div>
          <p className="landing-eyebrow">FRIDWIN CONTACTS</p>
          <h1 id="landing-title">Your contacts, all in one place.</h1>
          <p className="landing-description">
            Sign in to securely access and manage your contacts.
          </p>
          <a className="sso-button" href="/auth/login">
            <span aria-hidden="true">→</span>
            Sign in with FridWin
          </a>
        </section>
      </main>
    );
  }

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
        <div className="app-header-actions">
          <button type="button" className="ghost" onClick={refreshSyncAccounts}>
            Refresh sync
          </button>
          {view.mode === 'list' && !showDeleted && (
            <button type="button" onClick={() => setView({ mode: 'create' })}>
              Add contact
            </button>
          )}
        </div>
      </header>

      {error && (
        <div className="banner error" role="alert">
          {error}
          {error === 'Authentication required' && (
            <a href="/auth/login" className="login-link">
              Sign in with Authentik
            </a>
          )}
        </div>
      )}

      <section className="sync-panel" aria-labelledby="sync-panel-title">
        <div className="sync-panel-header">
          <div>
            <p className="section-eyebrow">SYNC</p>
            <h2 id="sync-panel-title">Connected accounts</h2>
          </div>
          <button type="button" onClick={handleConnectGoogle} disabled={syncConnecting}>
            {syncConnecting ? 'Starting Google…' : 'Connect Google'}
          </button>
        </div>
        <p className="sync-panel-description">
          Connect Google to sync your contacts. The authorization flow opens in a popup so your
          current app stays open.
        </p>
        {syncError && (
          <div className="banner error" role="alert">
            {syncError}
          </div>
        )}
        {syncLoading ? (
          <p>Loading sync accounts…</p>
        ) : syncAccounts.length === 0 ? (
          <p className="empty">No sync accounts configured yet.</p>
        ) : (
          <ul className="sync-account-list">
            {syncAccounts.map((account) => (
              <li key={account.id} className="sync-account-card">
                <div className="sync-account-head">
                  <strong>{account.provider}</strong>
                  <span className={`sync-status sync-status-${account.status}`}>
                    {account.status}
                  </span>
                </div>
                <dl className="sync-account-details">
                  <div>
                    <dt>Account</dt>
                    <dd>{account.provider_account_id}</dd>
                  </div>
                  <div>
                    <dt>Last sync</dt>
                    <dd>
                      {account.last_synced_at
                        ? new Date(account.last_synced_at).toLocaleString()
                        : 'Never'}
                    </dd>
                  </div>
                  <div>
                    <dt>Frequency</dt>
                    <dd>Every {account.sync_frequency_minutes} min</dd>
                  </div>
                  <div>
                    <dt>Cursor</dt>
                    <dd>{account.sync_cursor || '—'}</dd>
                  </div>
                </dl>
                {account.last_error && <p className="sync-account-error">{account.last_error}</p>}
              </li>
            ))}
          </ul>
        )}
      </section>

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

      <footer className="app-footer">
        <a href="/privacy">Privacy Policy</a>
      </footer>
    </main>
  );
}
