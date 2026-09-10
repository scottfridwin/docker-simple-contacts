import { useEffect, useState } from 'react';
import { createShare, deleteShare, listShares } from '../api';
import type { Share } from '../types';

interface SharesEditorProps {
  personId: string;
}

export function SharesEditor({ personId }: SharesEditorProps) {
  const [shares, setShares] = useState<Share[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [email, setEmail] = useState('');
  const [submitting, setSubmitting] = useState(false);

  const refresh = async () => {
    setLoading(true);
    setError(null);
    try {
      const res = await listShares(personId);
      setShares(res.data);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load shares');
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

  const handleAdd = async () => {
    setError(null);
    if (!email.trim()) {
      setError('Enter an email address.');
      return;
    }
    setSubmitting(true);
    try {
      await createShare(personId, email.trim());
      setEmail('');
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to share this contact');
    } finally {
      setSubmitting(false);
    }
  };

  const handleRevoke = async (share: Share) => {
    setError(null);
    try {
      await deleteShare(personId, share.id);
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to revoke access');
    }
  };

  return (
    <fieldset className="shares-editor" aria-label="sharing">
      <legend>Sharing</legend>
      {error && <span className="error">{error}</span>}
      {loading ? (
        <p>Loading shares…</p>
      ) : shares.length === 0 ? (
        <p className="empty">Not shared with anyone yet.</p>
      ) : (
        <ul className="share-list">
          {shares.map((s) => (
            <li key={s.id} className="share-item">
              <span>
                {s.display_name} ({s.email})
              </span>
              <button
                type="button"
                className="btn-icon btn-remove"
                onClick={() => handleRevoke(s)}
                aria-label={`revoke access for ${s.email}`}
              >
                ✕
              </button>
            </li>
          ))}
        </ul>
      )}

      <div className="share-add-form">
        <input
          aria-label="share with email"
          type="email"
          placeholder="person@example.com"
          value={email}
          onChange={(e) => setEmail(e.target.value)}
        />
        <button
          type="button"
          className="btn-icon btn-add"
          onClick={() => void handleAdd()}
          disabled={submitting}
          aria-label="share contact"
        >
          +
        </button>
      </div>
    </fieldset>
  );
}
