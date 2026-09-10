import type {
  CreatePersonInput,
  GoogleOAuthBeginResponse,
  Person,
  PersonListResponse,
  Relationship,
  RelationType,
  Share,
  SyncAccount,
  SyncAccountListResponse,
  UpdatePersonInput,
  ValidationDetail,
} from './types';

const API_BASE_URL = (import.meta.env.VITE_API_BASE_URL ?? '').replace(/\/$/, '');

/** Error raised for non-2xx API responses, carrying optional field details. */
export class ApiRequestError extends Error {
  code: string;
  status: number;
  details?: ValidationDetail[];

  constructor(status: number, code: string, message: string, details?: ValidationDetail[]) {
    super(message);
    this.name = 'ApiRequestError';
    this.status = status;
    this.code = code;
    this.details = details;
  }
}

/**
 * Extracts a user-facing message from a caught error, preferring the
 * specific per-field validation reason (e.g. "no account found for that
 * email") over the generic "request validation failed" envelope message.
 */
export function apiErrorMessage(err: unknown, fallback: string): string {
  if (err instanceof ApiRequestError) {
    if (err.details && err.details.length > 0) {
      return err.details.map((d) => d.message).join('; ');
    }
    return err.message;
  }
  if (err instanceof Error) {
    return err.message;
  }
  return fallback;
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`${API_BASE_URL}/api/v1${path}`, {
    headers: { 'Content-Type': 'application/json' },
    credentials: 'include',
    ...init,
  });

  if (res.status === 204) {
    return undefined as T;
  }

  const body = await res.json().catch(() => null);

  if (!res.ok) {
    const err = body?.error ?? {};
    throw new ApiRequestError(
      res.status,
      err.code ?? 'error',
      err.message ?? 'Request failed',
      Array.isArray(err.details) ? (err.details as ValidationDetail[]) : undefined,
    );
  }

  if (body === null) {
    throw new ApiRequestError(
      res.status,
      'invalid_response',
      'The server returned a non-JSON response',
    );
  }

  return body as T;
}

export interface ListPersonsParams {
  page?: number;
  pageSize?: number;
  sort?: string;
  order?: 'asc' | 'desc';
  firstName?: string;
  lastName?: string;
  favorite?: boolean;
}

export function listPersons(params: ListPersonsParams = {}): Promise<PersonListResponse> {
  const query = new URLSearchParams();
  if (params.page) query.set('page', String(params.page));
  if (params.pageSize) query.set('page_size', String(params.pageSize));
  if (params.sort) query.set('sort', params.sort);
  if (params.order) query.set('order', params.order);
  if (params.firstName) query.set('first_name', params.firstName);
  if (params.lastName) query.set('last_name', params.lastName);
  if (params.favorite !== undefined) query.set('favorite', String(params.favorite));
  const qs = query.toString();
  return request<PersonListResponse>(`/persons${qs ? `?${qs}` : ''}`);
}

export function getPerson(id: string): Promise<Person> {
  return request<Person>(`/persons/${id}`);
}

export function createPerson(input: CreatePersonInput): Promise<Person> {
  return request<Person>('/persons', {
    method: 'POST',
    body: JSON.stringify(input),
  });
}

export function updatePerson(id: string, input: UpdatePersonInput): Promise<Person> {
  return request<Person>(`/persons/${id}`, {
    method: 'PATCH',
    body: JSON.stringify(input),
  });
}

export function deletePerson(id: string): Promise<void> {
  return request<void>(`/persons/${id}`, { method: 'DELETE' });
}

export function listDeletedPersons(params: ListPersonsParams = {}): Promise<PersonListResponse> {
  const query = new URLSearchParams();
  if (params.page) query.set('page', String(params.page));
  if (params.pageSize) query.set('page_size', String(params.pageSize));
  if (params.sort) query.set('sort', params.sort);
  if (params.order) query.set('order', params.order);
  const qs = query.toString();
  return request<PersonListResponse>(`/persons/deleted${qs ? `?${qs}` : ''}`);
}

export function restorePerson(id: string): Promise<void> {
  return request<void>(`/persons/${id}/restore`, { method: 'POST' });
}

export function permanentlyDeletePerson(id: string): Promise<void> {
  return request<void>(`/persons/${id}/permanent`, { method: 'DELETE' });
}

export function listRelationships(personId: string): Promise<{ data: Relationship[] }> {
  return request<{ data: Relationship[] }>(`/persons/${personId}/relationships`);
}

export interface CreateRelationshipInput {
  type: RelationType;
  related_person_id?: string;
  related_person_name?: string;
}

export function createRelationship(
  personId: string,
  input: CreateRelationshipInput,
): Promise<Relationship> {
  return request<Relationship>(`/persons/${personId}/relationships`, {
    method: 'POST',
    body: JSON.stringify(input),
  });
}

export function deleteRelationship(personId: string, relationshipId: string): Promise<void> {
  return request<void>(`/persons/${personId}/relationships/${relationshipId}`, {
    method: 'DELETE',
  });
}

export function listShares(personId: string): Promise<{ data: Share[] }> {
  return request<{ data: Share[] }>(`/persons/${personId}/shares`);
}

export function createShare(personId: string, email: string): Promise<Share> {
  return request<Share>(`/persons/${personId}/shares`, {
    method: 'POST',
    body: JSON.stringify({ email }),
  });
}

export function deleteShare(personId: string, shareId: string): Promise<void> {
  return request<void>(`/persons/${personId}/shares/${shareId}`, { method: 'DELETE' });
}

/** Lets the current (recipient) account remove its own access to a contact shared with it. */
export function leaveShare(personId: string): Promise<void> {
  return request<void>(`/persons/${personId}/shares/mine`, { method: 'DELETE' });
}

export function listSyncAccounts(): Promise<SyncAccountListResponse> {
  return request<SyncAccountListResponse>('/sync-accounts');
}

export function updateSyncAccount(id: string, input: Partial<SyncAccount>): Promise<SyncAccount> {
  return request<SyncAccount>(`/sync-accounts/${id}`, {
    method: 'PATCH',
    body: JSON.stringify(input),
  });
}

export function deleteSyncAccount(id: string): Promise<void> {
  return request<void>(`/sync-accounts/${id}`, { method: 'DELETE' });
}

export function beginGoogleSync(redirectUri?: string): Promise<GoogleOAuthBeginResponse> {
  const query = new URLSearchParams();
  if (redirectUri) {
    query.set('redirect_uri', redirectUri);
  }
  const qs = query.toString();
  return request<GoogleOAuthBeginResponse>(`/sync/google/begin${qs ? `?${qs}` : ''}`);
}
