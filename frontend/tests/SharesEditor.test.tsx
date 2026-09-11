import { describe, expect, it, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { SharesEditor } from '../src/components/SharesEditor';
import { ApiRequestError } from '../src/api';
import type { Share } from '../src/types';

const mocks = vi.hoisted(() => ({
  listShares: vi.fn(),
  createShare: vi.fn(),
  deleteShare: vi.fn(),
}));

vi.mock('../src/api', async () => {
  const actual = await vi.importActual<typeof import('../src/api')>('../src/api');
  return {
    ...actual,
    listShares: mocks.listShares,
    createShare: mocks.createShare,
    deleteShare: mocks.deleteShare,
  };
});

function makeShare(overrides: Partial<Share> = {}): Share {
  return {
    id: 'share-1',
    email: 'friend@example.com',
    display_name: 'Friend',
    created_at: '2026-01-01T00:00:00Z',
    ...overrides,
  };
}

describe('SharesEditor', () => {
  it('lists existing shares', async () => {
    mocks.listShares.mockResolvedValue({ data: [makeShare()] });

    render(<SharesEditor personId="me" />);

    await waitFor(() => {
      expect(screen.getByText(/Friend \(friend@example.com\)/)).toBeInTheDocument();
    });
  });

  it('shares with a new email', async () => {
    mocks.listShares.mockResolvedValue({ data: [] });
    mocks.createShare.mockResolvedValue(makeShare());

    render(<SharesEditor personId="me" />);
    await waitFor(() => expect(screen.getByText(/not shared with anyone/i)).toBeInTheDocument());

    await userEvent.type(screen.getByLabelText('share with email'), 'friend@example.com');
    await userEvent.click(screen.getByRole('button', { name: /share contact/i }));

    await waitFor(() => {
      expect(mocks.createShare).toHaveBeenCalledWith('me', 'friend@example.com');
    });
  });

  it('revokes a share', async () => {
    mocks.listShares.mockResolvedValue({ data: [makeShare()] });
    mocks.deleteShare.mockResolvedValue(undefined);

    render(<SharesEditor personId="me" />);
    await waitFor(() => expect(screen.getByText(/Friend/)).toBeInTheDocument());

    await userEvent.click(
      screen.getByRole('button', { name: /revoke access for friend@example.com/i }),
    );

    await waitFor(() => {
      expect(mocks.deleteShare).toHaveBeenCalledWith('me', 'share-1');
    });
  });

  it('shows the specific validation reason instead of the generic envelope message', async () => {
    mocks.listShares.mockResolvedValue({ data: [] });
    mocks.createShare.mockRejectedValue(
      new ApiRequestError(422, 'validation_error', 'request validation failed', [
        { field: 'email', message: 'no account found for that email' },
      ]),
    );

    render(<SharesEditor personId="me" />);
    await waitFor(() => expect(screen.getByText(/not shared with anyone/i)).toBeInTheDocument());

    await userEvent.type(screen.getByLabelText('share with email'), 'nobody@example.com');
    await userEvent.click(screen.getByRole('button', { name: /share contact/i }));

    await waitFor(() => {
      expect(screen.getByText(/no account found for that email/i)).toBeInTheDocument();
    });
    expect(screen.queryByText(/request validation failed/i)).not.toBeInTheDocument();
  });
});
