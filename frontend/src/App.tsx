import { useCallback, useEffect, useState } from 'react';
import {
  ApiRequestError,
  apiErrorMessage,
  beginGoogleSync,
  createPerson,
  deletePerson,
  deleteSyncAccount,
  getPerson,
  leaveShare,
  listDeletedPersons,
  listSyncAccounts,
  listPersons,
  permanentlyDeletePerson,
  restorePerson,
  updatePerson,
  updateSyncAccount,
} from './api';
import type { Person, SyncAccount } from './types';
import { PersonForm, type PersonFormValues } from './components/PersonForm';
import { PersonList } from './components/PersonList';
import { CloseIcon, PlusIcon, SyncIcon, TrashIcon } from './icons';

type View = { mode: 'list' } | { mode: 'create' } | { mode: 'edit'; person: Person };

type SyncAccountDraft = {
  sync_frequency_minutes: string;
};

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

function HomeInfoPage() {
  return (
    <main className="landing-page">
      <section className="landing-card" aria-labelledby="landing-title">
        <div className="landing-banner" aria-hidden="true">
          <span className="landing-mark">FW</span>
        </div>
        <p className="landing-eyebrow">FRIDWIN CONTACTS</p>
        <h1 id="landing-title">Family contact manager with optional Google sync</h1>
        <p className="landing-description">
          This private app helps a small family group store contact details, keep records organized,
          and optionally sync with Google Contacts.
        </p>
        <div className="landing-purpose" aria-label="app purpose">
          <p>
            You can review this page without signing in. Sign-in is only required to access and edit
            private contact data.
          </p>
          <ul>
            <li>Manage names, phone numbers, and custom profile fields.</li>
            <li>Connect Google Contacts to synchronize records you choose.</li>
            <li>Control your own data retention with soft delete and purge behavior.</li>
          </ul>
        </div>
        <div className="landing-actions">
          <a className="sso-button" href="/auth/login">
            <span aria-hidden="true">→</span>
            Sign in with FridWin
          </a>
          <a className="landing-link" href="/privacy">
            Privacy Policy
          </a>
        </div>
      </section>
    </main>
  );
}

export default function App() {
  const isPrivacyPage = getAppPathname() === '/privacy';
  const [persons, setPersons] = useState<Person[]>([]);
  const [syncAccounts, setSyncAccounts] = useState<SyncAccount[]>([]);
  const [syncDrafts, setSyncDrafts] = useState<Record<string, SyncAccountDraft>>({});
  const [syncDrawerOpen, setSyncDrawerOpen] = useState(false);
  const [view, setView] = useState<View>({ mode: 'list' });
  const [personsLoaded, setPersonsLoaded] = useState(false);
  const [syncLoading, setSyncLoading] = useState(false);
  const [syncLoaded, setSyncLoaded] = useState(false);
  const [syncConnecting, setSyncConnecting] = useState(false);
  const [syncSavingId, setSyncSavingId] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [syncError, setSyncError] = useState<string | null>(null);
  const [authenticationRequired, setAuthenticationRequired] = useState(false);
  const [serverErrors, setServerErrors] = useState<Record<string, string>>({});
  const [search, setSearch] = useState({ firstName: '', lastName: '' });
  const [page, setPage] = useState(1);
  const [totalPages, setTotalPages] = useState(1);
  const [showDeleted, setShowDeleted] = useState(false);
  const [favorites, setFavorites] = useState<Person[]>([]);
  const [favoritesError, setFavoritesError] = useState<string | null>(null);

  const refresh = useCallback(async () => {
    if (isPrivacyPage) {
      return;
    }
    setError(null);
    try {
      const loader = showDeleted ? listDeletedPersons : listPersons;
      const res = await loader({
        page,
        sort: 'last_name',
        order: 'asc',
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
      setPersonsLoaded(true);
    }
  }, [isPrivacyPage, page, search, showDeleted]);

  // Favorites are always shown in their own section regardless of the main
  // list's current page, sort, or search - so they're fetched independently.
  const refreshFavorites = useCallback(async () => {
    if (isPrivacyPage) {
      return;
    }
    try {
      const res = await listPersons({
        favorite: true,
        pageSize: 100,
        sort: 'last_name',
        order: 'asc',
      });
      setFavorites(res.data);
      setFavoritesError(null);
    } catch (err) {
      if (!(err instanceof ApiRequestError && err.status === 401)) {
        setFavoritesError(err instanceof Error ? err.message : 'Failed to load favorites');
      }
    }
  }, [isPrivacyPage]);

  const refreshSyncAccounts = useCallback(async () => {
    if (isPrivacyPage) {
      return;
    }
    setSyncLoading(true);
    setSyncError(null);
    try {
      const res = await listSyncAccounts();
      setSyncAccounts(res.data);
      setSyncDrafts((prev) => {
        const next: Record<string, SyncAccountDraft> = {};
        for (const account of res.data) {
          const committed = String(account.sync_frequency_minutes);
          const existing = prev[account.id];
          // Keep an in-progress edit instead of clobbering it with a background poll.
          next[account.id] =
            existing && existing.sync_frequency_minutes !== committed
              ? existing
              : { sync_frequency_minutes: committed };
        }
        return next;
      });
    } catch (err) {
      if (err instanceof ApiRequestError && err.status === 401) {
        setSyncAccounts([]);
      } else {
        setSyncError(err instanceof Error ? err.message : 'Failed to load sync accounts');
      }
    } finally {
      setSyncLoading(false);
      setSyncLoaded(true);
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
    queueMicrotask(() => {
      void refreshFavorites();
    });
  }, [isPrivacyPage, refreshFavorites]);

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
      void refresh();
      void refreshSyncAccounts();
    };
    window.addEventListener('focus', onFocus);
    return () => window.removeEventListener('focus', onFocus);
  }, [isPrivacyPage, refresh, refreshSyncAccounts]);

  useEffect(() => {
    if (isPrivacyPage) {
      return;
    }
    const onMessage = (event: MessageEvent) => {
      if (event.origin !== window.location.origin) return;
      if (event.data?.type === 'google-sync-complete') {
        void refreshSyncAccounts();
        void refresh();
      }
    };
    window.addEventListener('message', onMessage);
    return () => window.removeEventListener('message', onMessage);
  }, [isPrivacyPage, refresh, refreshSyncAccounts]);

  // Poll for live sync status: fast while a first sync is still in flight,
  // slower otherwise, so the panel updates itself like an app instead of
  // requiring a manual page reload.
  useEffect(() => {
    if (isPrivacyPage || authenticationRequired) {
      return;
    }
    const hasPendingSync = syncAccounts.some(
      (account) =>
        !account.last_synced_at &&
        account.status !== 'reconnect_required' &&
        account.status !== 'error',
    );
    const delay = hasPendingSync ? 3000 : 30000;
    const id = window.setInterval(() => {
      if (document.hidden) return;
      void refreshSyncAccounts();
      // A connected account can pull in new contacts on its own schedule (not
      // just right after connecting), so keep the contacts list fresh too
      // instead of requiring a manual reload.
      if (syncAccounts.length > 0) {
        void refresh();
      }
    }, delay);
    return () => window.clearInterval(id);
  }, [authenticationRequired, isPrivacyPage, refresh, refreshSyncAccounts, syncAccounts]);

  useEffect(() => {
    if (!syncDrawerOpen) {
      return;
    }
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setSyncDrawerOpen(false);
    };
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, [syncDrawerOpen]);

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

  const updateSyncDraft = (id: string, patch: Partial<SyncAccountDraft>) => {
    setSyncDrafts((prev) => ({
      ...prev,
      [id]: { ...prev[id], ...patch },
    }));
  };

  const getSyncDraftState = (account: SyncAccount) => {
    const draft = syncDrafts[account.id];
    const frequency = Number(draft?.sync_frequency_minutes ?? account.sync_frequency_minutes);
    const validFrequency =
      Number.isFinite(frequency) && Number.isInteger(frequency) && frequency >= 5;
    const changed = frequency !== account.sync_frequency_minutes;
    return { draft, frequency, validFrequency, changed };
  };

  const saveSyncAccount = async (account: SyncAccount) => {
    const { frequency, validFrequency, changed } = getSyncDraftState(account);
    if (!changed || !validFrequency) return;
    setSyncError(null);
    setSyncSavingId(account.id);
    try {
      await updateSyncAccount(account.id, {
        sync_frequency_minutes: frequency,
      });
      await refreshSyncAccounts();
    } catch (err) {
      setSyncError(err instanceof Error ? err.message : 'Failed to update sync account');
    } finally {
      setSyncSavingId((prev) => (prev === account.id ? null : prev));
    }
  };

  const handleDisconnectAccount = async (account: SyncAccount) => {
    const label = account.display_name || account.provider_account_id || account.provider;
    if (!window.confirm(`Disconnect ${label}? This stops syncing this account.`)) {
      return;
    }
    setSyncError(null);
    setSyncSavingId(account.id);
    try {
      await deleteSyncAccount(account.id);
      await refreshSyncAccounts();
    } catch (err) {
      setSyncError(err instanceof Error ? err.message : 'Failed to disconnect sync account');
    } finally {
      setSyncSavingId((prev) => (prev === account.id ? null : prev));
    }
  };

  if (authenticationRequired) {
    return <HomeInfoPage />;
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
        emails: values.emails.length ? values.emails : undefined,
        phone_numbers: values.phone_numbers.length ? values.phone_numbers : undefined,
        addresses: values.addresses.length ? values.addresses : undefined,
        organization:
          values.organization.name || values.organization.title || values.organization.department
            ? values.organization
            : undefined,
        notes: values.notes || undefined,
        custom_fields: values.custom_fields,
        labels: values.labels.length ? values.labels : undefined,
      };
      if (view.mode === 'edit') {
        await updatePerson(view.person.id, payload);
      } else {
        await createPerson(payload);
      }
      setView({ mode: 'list' });
      await refresh();
      await refreshFavorites();
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
      await refreshFavorites();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Delete failed');
    }
  };

  const handleLeaveShare = async (person: Person) => {
    if (
      !window.confirm(
        `Remove ${person.display_name} from your contacts? You can ask the owner to re-share it later.`,
      )
    ) {
      return;
    }
    setError(null);
    try {
      await leaveShare(person.id);
      setView({ mode: 'list' });
      await refresh();
      await refreshFavorites();
    } catch (err) {
      setError(apiErrorMessage(err, 'Failed to remove this shared contact'));
    }
  };

  const handleRestore = async (person: Person) => {
    setError(null);
    try {
      await restorePerson(person.id);
      await refresh();
      await refreshFavorites();
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
      await refreshFavorites();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Permanent delete failed');
    }
  };

  const handleToggleFavorite = async (person: Person) => {
    setError(null);
    try {
      await updatePerson(person.id, { is_favorite: !person.is_favorite });
      await refresh();
      await refreshFavorites();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to update favorite');
    }
  };

  const handleNavigateToPerson = async (personId: string) => {
    setError(null);
    try {
      const person = await getPerson(personId);
      setView({ mode: 'edit', person });
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to open contact');
    }
  };

  const hasReconnectIssue = syncAccounts.some(
    (account) => account.status === 'error' || account.status === 'reconnect_required',
  );
  const overallSyncState: 'none' | 'syncing' | 'error' | 'connected' =
    syncAccounts.length === 0
      ? 'none'
      : hasReconnectIssue
        ? 'error'
        : syncAccounts.some((account) => !account.last_synced_at)
          ? 'syncing'
          : 'connected';
  const syncIndicatorLabel: Record<typeof overallSyncState, string> = {
    none: 'not connected',
    syncing: 'syncing',
    error: 'needs attention',
    connected: 'connected',
  };

  return (
    <main className="app">
      <header className="app-header">
        <div className="app-header-title">
          <button
            type="button"
            className="icon-button"
            onClick={() => setSyncDrawerOpen(true)}
            aria-label={`Open sync settings (${syncIndicatorLabel[overallSyncState]})`}
          >
            <SyncIcon />
            {overallSyncState !== 'none' && (
              <span
                className={`sync-indicator-dot sync-indicator-${overallSyncState}`}
                aria-hidden="true"
              />
            )}
          </button>
          <h1>Contacts</h1>
        </div>
        {view.mode === 'list' && (
          <div className="app-header-actions">
            {!showDeleted && (
              <button
                type="button"
                className="btn-with-icon"
                onClick={() => setView({ mode: 'create' })}
              >
                <PlusIcon />
                Add contact
              </button>
            )}
            <button
              type="button"
              className="ghost btn-with-icon"
              onClick={() => {
                setPage(1);
                setSearch({ firstName: '', lastName: '' });
                setShowDeleted((v) => !v);
              }}
            >
              <TrashIcon />
              {showDeleted ? 'Active contacts' : 'Recycle bin'}
            </button>
          </div>
        )}
      </header>

      {error && (
        <div className="banner error" role="alert">
          {error}
          {error === 'Authentication required' ? (
            <a href="/auth/login" className="login-link">
              Sign in with Authentik
            </a>
          ) : (
            <button type="button" className="ghost" onClick={() => void refresh()}>
              Retry
            </button>
          )}
        </div>
      )}

      {syncDrawerOpen && (
        <div
          className="drawer-backdrop"
          onClick={() => setSyncDrawerOpen(false)}
          aria-hidden="true"
        />
      )}
      <aside
        className={`sync-drawer${syncDrawerOpen ? ' sync-drawer-open' : ''}`}
        aria-label="Sync settings"
        aria-hidden={!syncDrawerOpen}
        inert={!syncDrawerOpen}
      >
        <div className="sync-drawer-header">
          <div>
            <p className="section-eyebrow">SYNC</p>
            <h2 id="sync-panel-title">Connected accounts</h2>
          </div>
          <button
            type="button"
            className="icon-button"
            onClick={() => setSyncDrawerOpen(false)}
            aria-label="Close sync settings"
          >
            <CloseIcon />
          </button>
        </div>
        <p className="sync-panel-description">
          Connect Google to sync your contacts. You can connect more than one Google account — each
          one mirrors your full contact list. The authorization flow opens in a popup so your
          current app stays open.
        </p>
        {syncError && (
          <div className="banner error" role="alert">
            {syncError}
            <button type="button" className="ghost" onClick={() => void refreshSyncAccounts()}>
              Retry
            </button>
          </div>
        )}
        <div className="sync-drawer-actions">
          <button type="button" onClick={handleConnectGoogle} disabled={syncConnecting}>
            {syncConnecting
              ? 'Starting Google…'
              : syncAccounts.length === 0
                ? 'Connect Google'
                : 'Add Google account'}
          </button>
          <button
            type="button"
            className="ghost"
            onClick={() => void refreshSyncAccounts()}
            disabled={syncLoading}
          >
            {syncLoading ? 'Refreshing…' : 'Refresh sync'}
          </button>
        </div>
        {!syncLoaded ? (
          <p>Loading sync accounts…</p>
        ) : syncAccounts.length === 0 ? (
          <p className="empty">No sync accounts configured yet.</p>
        ) : (
          <ul className="sync-account-list">
            {syncAccounts.map((account) => {
              const state = getSyncDraftState(account);
              const disabled =
                syncSavingId === account.id || !state.changed || !state.validFrequency;
              const isSyncing =
                !account.last_synced_at &&
                account.status !== 'reconnect_required' &&
                account.status !== 'error';
              return (
                <li key={account.id} className="sync-account-card">
                  <div className="sync-account-head">
                    <strong>{account.display_name || account.provider}</strong>
                    <span className={`sync-status sync-status-${account.status}`}>
                      {account.status}
                    </span>
                  </div>
                  <dl className="sync-account-details">
                    <div>
                      <dt>Last sync</dt>
                      <dd>
                        {isSyncing ? (
                          <span className="sync-live-indicator">
                            <span className="sync-live-dot" aria-hidden="true" />
                            Syncing…
                          </span>
                        ) : account.last_synced_at ? (
                          new Date(account.last_synced_at).toLocaleString()
                        ) : (
                          'Never'
                        )}
                      </dd>
                    </div>
                    <div>
                      <dt>Frequency (min)</dt>
                      <dd>
                        <input
                          aria-label="Sync frequency minutes"
                          type="number"
                          min={5}
                          step={1}
                          value={
                            syncDrafts[account.id]?.sync_frequency_minutes ??
                            account.sync_frequency_minutes
                          }
                          onChange={(e) =>
                            updateSyncDraft(account.id, {
                              sync_frequency_minutes: e.target.value,
                            })
                          }
                        />
                      </dd>
                    </div>
                  </dl>
                  <div className="sync-account-actions">
                    {(account.status === 'error' || account.status === 'reconnect_required') && (
                      <button
                        type="button"
                        className="ghost"
                        onClick={handleConnectGoogle}
                        disabled={syncConnecting}
                      >
                        Reconnect
                      </button>
                    )}
                    <button
                      type="button"
                      className="ghost danger"
                      onClick={() => void handleDisconnectAccount(account)}
                      disabled={syncSavingId === account.id}
                    >
                      Disconnect
                    </button>
                    <button
                      type="button"
                      onClick={() => void saveSyncAccount(account)}
                      disabled={disabled}
                    >
                      {syncSavingId === account.id ? 'Saving…' : 'Save changes'}
                    </button>
                  </div>
                  {account.last_error && <p className="sync-account-error">{account.last_error}</p>}
                </li>
              );
            })}
          </ul>
        )}
      </aside>

      {view.mode === 'list' && (
        <>
          {!showDeleted && favorites.length > 0 && (
            <section className="favorites-section" aria-label="favorites">
              <h2>Favorites</h2>
              <PersonList
                persons={favorites}
                onEdit={(person) => setView({ mode: 'edit', person })}
                onDelete={handleDelete}
                onToggleFavorite={handleToggleFavorite}
              />
            </section>
          )}
          {favoritesError && <p className="error">{favoritesError}</p>}
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
          </div>
          {!personsLoaded ? (
            <p>Loading…</p>
          ) : (
            <PersonList
              persons={persons}
              onEdit={(person) => setView({ mode: 'edit', person })}
              onDelete={handleDelete}
              deleted={showDeleted}
              onRestore={handleRestore}
              onPermanentDelete={handlePermanentDelete}
              onToggleFavorite={handleToggleFavorite}
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
            onNavigateToPerson={handleNavigateToPerson}
            onLeaveShare={handleLeaveShare}
          />
        </section>
      )}

      <footer className="app-footer">
        <a href="/privacy">Privacy Policy</a>
      </footer>
    </main>
  );
}
