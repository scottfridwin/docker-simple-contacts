export type CustomFieldType = 'string' | 'number' | 'boolean' | 'date';

export type CustomFieldValue = string | number | boolean;

export type AddressType = 'home' | 'work' | 'other';

export interface Address {
  type: AddressType;
  street: string;
  city: string;
  state?: string;
  postal_code?: string;
  country?: string;
  label?: string | null;
}

export type RelationshipType = 'spouse' | 'parent' | 'child' | 'sibling' | 'colleague' | 'friend' | 'other';

export interface Relationship {
  id: string;
  person_id_1: string;
  person_id_2: string;
  relationship_type: RelationshipType;
  label?: string | null;
  created_at: string;
  updated_at: string;
  deleted_at?: string | null;
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
  phone_numbers: string[];
  addresses: Address[];
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
  phone_numbers?: string[];
  addresses?: Address[];
  custom_fields?: Record<string, CustomFieldValue>;
}

export type UpdatePersonInput = Partial<CreatePersonInput>;

export interface CreateRelationshipInput {
  person_id_2: string;
  relationship_type: RelationshipType;
  label?: string;
}

export type UpdateRelationshipInput = Partial<CreateRelationshipInput>;

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
