import { describe, expect, it, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import App from '../src/App';
import type { PersonListResponse, SyncAccountListResponse } from '../src/types';

const mocks = vi.hoisted(() => ({
  listPersons: vi.fn(),
  listDeletedPersons: vi.fn(),
  listSyncAccounts: vi.fn(),
  beginGoogleSync: vi.fn(),
  createPerson: vi.fn(),
  deletePerson: vi.fn(),
  permanentlyDeletePerson: vi.fn(),
  restorePerson: vi.fn(),
  updatePerson: vi.fn(),
}));

vi.mock('../src/api', () => ({
  ApiRequestError: class ApiRequestError extends Error {
    status: number;
    code: string;
    details?: unknown;

    constructor(status: number, code: string, message: string, details?: unknown) {
      super(message);
      this.status = status;
      this.code = code;
      this.details = details;
    }
  },
  listPersons: mocks.listPersons,
  listDeletedPersons: mocks.listDeletedPersons,
  listSyncAccounts: mocks.listSyncAccounts,
  beginGoogleSync: mocks.beginGoogleSync,
  createPerson: mocks.createPerson,
  deletePerson: mocks.deletePerson,
  permanentlyDeletePerson: mocks.permanentlyDeletePerson,
  restorePerson: mocks.restorePerson,
  updatePerson: mocks.updatePerson,
}));

describe('App sync integration', () => {
  beforeEach(() => {
    vi.resetAllMocks();
    const persons: PersonListResponse = {
      data: [],
      page: 1,
      page_size: 25,
      total: 0,
      total_pages: 0,
    };
    const syncAccounts: SyncAccountListResponse = { data: [] };
    mocks.listPersons.mockResolvedValue(persons);
    mocks.listDeletedPersons.mockResolvedValue(persons);
    mocks.listSyncAccounts.mockResolvedValue(syncAccounts);
    mocks.beginGoogleSync.mockResolvedValue({
      authorization_url: 'https://accounts.google.com/o/oauth2/v2/auth?state=abc',
      state: 'abc',
    });
    Object.defineProperty(window, 'open', {
      value: vi.fn().mockReturnValue({ close: vi.fn() }),
      writable: true,
    });
    Object.defineProperty(window, 'location', {
      value: { origin: 'https://contacts.example', pathname: '/', assign: vi.fn() },
      writable: true,
    });
  });

  it('renders the privacy policy at /privacy without loading app data', () => {
    Object.defineProperty(window, 'location', {
      value: { origin: 'https://contacts.example', pathname: '/privacy', assign: vi.fn() },
      writable: true,
    });

    render(<App />);

    expect(screen.getByRole('heading', { name: /privacy policy/i })).toBeInTheDocument();
    expect(screen.getByText(/family and direct-relations group/i)).toBeInTheDocument();
    expect(mocks.listPersons).not.toHaveBeenCalled();
    expect(mocks.listSyncAccounts).not.toHaveBeenCalled();
  });

  it('renders sync panel and configured accounts', async () => {
    mocks.listSyncAccounts.mockResolvedValueOnce({
      data: [
        {
          id: '1',
          owner_id: null,
          provider: 'google',
          provider_account_id: 'people/abc',
          access_token: null,
          refresh_token: null,
          expires_at: null,
          scope: 'contacts',
          sync_cursor: 'cursor-1',
          sync_frequency_minutes: 30,
          status: 'connected',
          last_synced_at: '2026-09-08T12:00:00Z',
          last_error: null,
          created_at: '2026-09-08T11:00:00Z',
          updated_at: '2026-09-08T12:00:00Z',
        },
      ],
    });

    render(<App />);

    expect(await screen.findByRole('heading', { name: /connected accounts/i })).toBeInTheDocument();
    expect(screen.getByText('google')).toBeInTheDocument();
    expect(screen.getByText('connected')).toBeInTheDocument();
    expect(screen.getByText(/people\/abc/i)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /connect google/i })).toBeInTheDocument();
  });

  it('starts the google connect flow from the ui', async () => {
    render(<App />);

    await userEvent.click(screen.getByRole('button', { name: /connect google/i }));

    await waitFor(() => {
      expect(mocks.beginGoogleSync).toHaveBeenCalledWith('https://contacts.example/api/v1/sync/google/callback');
    });
    expect(window.open).toHaveBeenCalledWith(
      'https://accounts.google.com/o/oauth2/v2/auth?state=abc',
      'google-sync',
      'popup,width=520,height=760',
    );
    expect(window.location.assign).not.toHaveBeenCalled();
  });

  it('falls back to same-tab navigation when popup is blocked', async () => {
    (window.open as unknown as ReturnType<typeof vi.fn>).mockReturnValueOnce(null);
    render(<App />);

    await userEvent.click(screen.getByRole('button', { name: /connect google/i }));

    await waitFor(() => {
      expect(window.location.assign).toHaveBeenCalledWith(
        'https://accounts.google.com/o/oauth2/v2/auth?state=abc',
      );
    });
  });
});
