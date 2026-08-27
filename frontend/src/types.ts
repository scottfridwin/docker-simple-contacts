export type CustomFieldType = 'string' | 'number' | 'boolean' | 'date';

export type CustomFieldValue = string | number | boolean;

export interface Person {
  id: string;
  first_name: string;
  middle_names: string[];
  last_name: string;
  display_name: string;
  nickname?: string | null;
  pronouns?: string | null;
  birthdate?: string | null;
  custom_fields: Record<string, CustomFieldValue>;
  created_at: string;
  updated_at: string;
  deleted_at?: string | null;
}

export interface PersonListResponse {
  data: Person[];
  page: number;
  page_size: number;
  total: number;
  total_pages: number;
}

export interface CreatePersonInput {
  first_name: string;
  middle_names?: string[];
  last_name: string;
  nickname?: string;
  pronouns?: string;
  birthdate?: string;
  custom_fields?: Record<string, CustomFieldValue>;
}

export type UpdatePersonInput = Partial<CreatePersonInput>;

export interface ApiError {
  error: {
    code: string;
    message: string;
    details?: unknown;
  };
}

export interface ValidationDetail {
  field: string;
  message: string;
}
