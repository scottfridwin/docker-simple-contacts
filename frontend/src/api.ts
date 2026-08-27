import type {
  CreatePersonInput,
  CreateRelationshipInput,
  Person,
  PersonListResponse,
  Relationship,
  UpdatePersonInput,
  UpdateRelationshipInput,
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

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`${API_BASE_URL}/api/v1${path}`, {
    headers: { 'Content-Type': 'application/json' },
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
}

export function listPersons(params: ListPersonsParams = {}): Promise<PersonListResponse> {
  const query = new URLSearchParams();
  if (params.page) query.set('page', String(params.page));
  if (params.pageSize) query.set('page_size', String(params.pageSize));
  if (params.sort) query.set('sort', params.sort);
  if (params.order) query.set('order', params.order);
  if (params.firstName) query.set('first_name', params.firstName);
  if (params.lastName) query.set('last_name', params.lastName);
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

export function listRelationshipsForPerson(personId: string): Promise<Relationship[]> {
  return request<Relationship[]>(`/persons/${personId}/relationships`);
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

export function getRelationship(id: string): Promise<Relationship> {
  return request<Relationship>(`/relationships/${id}`);
}

export function updateRelationship(id: string, input: UpdateRelationshipInput): Promise<Relationship> {
  return request<Relationship>(`/relationships/${id}`, {
    method: 'PATCH',
    body: JSON.stringify(input),
  });
}

export function deleteRelationship(id: string): Promise<void> {
  return request<void>(`/relationships/${id}`, { method: 'DELETE' });
}
