export type CustomFieldType = 'string' | 'number' | 'boolean' | 'date';

export type CustomFieldValue = string | number | boolean;

export interface LabeledValue {
  label: string;
  value: string;
}

export interface Address {
  label?: string;
  street?: string;
  city?: string;
  region?: string;
  postal_code?: string;
  country?: string;
}

export interface Organization {
  name?: string;
  title?: string;
  department?: string;
}

export type RelationType = 'parent' | 'child' | 'spouse' | 'sibling' | 'partner';

export const RELATION_TYPES: RelationType[] = ['parent', 'child', 'spouse', 'sibling', 'partner'];

export interface Relationship {
  id: string;
  type: RelationType;
  related_person_id?: string | null;
  related_person_name: string;
  related_person_deleted: boolean;
}

export interface Share {
  id: string;
  email: string;
  display_name: string;
  created_at: string;
}

export interface Person {
  id: string;
  first_name: string;
  middle_names: string[];
  last_name: string;
  display_name: string;
  nickname?: string | null;
  pronouns?: string | null;
  birthdate?: string | null;
  emails: LabeledValue[];
  phone_numbers: LabeledValue[];
  addresses: Address[];
  organization?: Organization | null;
  notes?: string | null;
  custom_fields: Record<string, CustomFieldValue>;
  labels: string[];
  is_favorite: boolean;
  is_owner?: boolean;
  owner_display_name?: string | null;
  created_at: string;
  updated_at: string;
  deleted_at?: string | null;
}

export interface SyncMetadata {
  googleResourceName?: string | null;
  googleUpdatedAt?: string | null;
}

export interface PersonListResponse {
  data: Person[];
  page: number;
  page_size: number;
  total: number;
  total_pages: number;
}

export interface SyncAccount {
  id: string;
  owner_id?: string | null;
  provider: string;
  provider_account_id: string;
  display_name?: string | null;
  expires_at?: string | null;
  scope: string;
  sync_cursor: string;
  sync_frequency_minutes: number;
  status: string;
  last_synced_at?: string | null;
  last_error?: string | null;
  created_at: string;
  updated_at: string;
}

export interface SyncAccountListResponse {
  data: SyncAccount[];
}

export interface GoogleOAuthBeginResponse {
  authorization_url: string;
  state: string;
}

export interface CreatePersonInput {
  first_name: string;
  middle_names?: string[];
  last_name: string;
  nickname?: string;
  pronouns?: string;
  birthdate?: string;
  emails?: LabeledValue[];
  phone_numbers?: LabeledValue[];
  addresses?: Address[];
  organization?: Organization | null;
  notes?: string;
  custom_fields?: Record<string, CustomFieldValue>;
  labels?: string[];
  is_favorite?: boolean;
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
