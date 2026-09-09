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
  updateSyncAccount: vi.fn(),
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
  updateSyncAccount: mocks.updateSyncAccount,
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
    mocks.updateSyncAccount.mockResolvedValue({});
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

  it('shows public app purpose content when authentication is required', async () => {
    const { ApiRequestError } = await import('../src/api');
    mocks.listPersons.mockRejectedValueOnce(
      new ApiRequestError(401, 'unauthorized', 'Unauthorized'),
    );

    render(<App />);

    expect(
      await screen.findByRole('heading', {
        name: /family contact manager with optional google sync/i,
      }),
    ).toBeInTheDocument();
    expect(screen.getByText(/review this page without signing in/i)).toBeInTheDocument();
    expect(screen.getByRole('link', { name: /privacy policy/i })).toHaveAttribute(
      'href',
      '/privacy',
    );
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
    await userEvent.click(screen.getByRole('button', { name: /open sync settings/i }));

    expect(await screen.findByRole('heading', { name: /connected accounts/i })).toBeInTheDocument();
    expect(screen.getByText('google')).toBeInTheDocument();
    expect(screen.getByRole('spinbutton', { name: /sync frequency minutes/i })).toHaveValue(30);
    expect(screen.getByRole('button', { name: /connect google/i })).toBeInTheDocument();
  });

  it('starts the google connect flow from the ui', async () => {
    render(<App />);
    await userEvent.click(screen.getByRole('button', { name: /open sync settings/i }));

    await userEvent.click(screen.getByRole('button', { name: /connect google/i }));

    await waitFor(() => {
      expect(mocks.beginGoogleSync).toHaveBeenCalledWith(
        'https://contacts.example/api/v1/sync/google/callback',
      );
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
    await userEvent.click(screen.getByRole('button', { name: /open sync settings/i }));

    await userEvent.click(screen.getByRole('button', { name: /connect google/i }));

    await waitFor(() => {
      expect(window.location.assign).toHaveBeenCalledWith(
        'https://accounts.google.com/o/oauth2/v2/auth?state=abc',
      );
    });
  });

  it('opens and closes the sync drawer', async () => {
    render(<App />);

    expect(screen.queryByRole('heading', { name: /connected accounts/i })).not.toBeInTheDocument();

    await userEvent.click(screen.getByRole('button', { name: /open sync settings/i }));
    expect(await screen.findByRole('heading', { name: /connected accounts/i })).toBeInTheDocument();

    await userEvent.click(screen.getByRole('button', { name: /close sync settings/i }));
    await waitFor(() => {
      expect(
        screen.queryByRole('heading', { name: /connected accounts/i }),
      ).not.toBeInTheDocument();
    });
  });

  it('enables save only after account changes and submits updated values', async () => {
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
    await userEvent.click(screen.getByRole('button', { name: /open sync settings/i }));

    const saveButton = await screen.findByRole('button', { name: /save changes/i });
    expect(saveButton).toBeDisabled();

    await userEvent.clear(screen.getByRole('spinbutton', { name: /sync frequency minutes/i }));
    await userEvent.type(screen.getByRole('spinbutton', { name: /sync frequency minutes/i }), '45');

    expect(saveButton).toBeEnabled();
    await userEvent.click(saveButton);

    await waitFor(() => {
      expect(mocks.updateSyncAccount).toHaveBeenCalledWith('1', {
        sync_frequency_minutes: 45,
      });
    });
  });

  it('shows a live syncing indicator before the first background sync completes', async () => {
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
          sync_cursor: '',
          sync_frequency_minutes: 5,
          status: 'connected',
          last_synced_at: null,
          last_error: null,
          created_at: '2026-09-08T11:00:00Z',
          updated_at: '2026-09-08T11:00:00Z',
        },
      ],
    });

    render(<App />);
    await userEvent.click(screen.getByRole('button', { name: /open sync settings/i }));

    expect(await screen.findByText(/syncing/i)).toBeInTheDocument();
  });
});
