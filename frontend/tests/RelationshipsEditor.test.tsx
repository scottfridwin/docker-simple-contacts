import { beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { RelationshipsEditor } from '../src/components/RelationshipsEditor';
import type { Person, PersonListResponse, Relationship } from '../src/types';

const mocks = vi.hoisted(() => ({
  listRelationships: vi.fn(),
  createRelationship: vi.fn(),
  deleteRelationship: vi.fn(),
  listPersons: vi.fn(),
}));

vi.mock('../src/api', () => ({
  listRelationships: mocks.listRelationships,
  createRelationship: mocks.createRelationship,
  deleteRelationship: mocks.deleteRelationship,
  listPersons: mocks.listPersons,
}));

function makePerson(overrides: Partial<Person> = {}): Person {
  return {
    id: 'other-1',
    first_name: 'Jane',
    middle_names: [],
    last_name: 'Doe',
    display_name: 'Jane Doe',
    emails: [],
    phone_numbers: [],
    addresses: [],
    custom_fields: {},
    is_favorite: false,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    ...overrides,
  };
}

function makePersonsResponse(data: Person[]): PersonListResponse {
  return { data, page: 1, page_size: 8, total: data.length, total_pages: 1 };
}

describe('RelationshipsEditor', () => {
  beforeEach(() => {
    mocks.listPersons.mockReset().mockResolvedValue(makePersonsResponse([]));
  });

  it('lists existing relationships and links to a related contact', async () => {
    const relationships: Relationship[] = [
      {
        id: 'rel-1',
        type: 'spouse',
        related_person_id: 'other-1',
        related_person_name: 'Jane Doe',
        related_person_deleted: false,
      },
    ];
    mocks.listRelationships.mockResolvedValue({ data: relationships });
    const onNavigate = vi.fn();

    render(<RelationshipsEditor personId="me" onNavigateToPerson={onNavigate} />);

    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Jane Doe' })).toBeInTheDocument();
    });

    await userEvent.click(screen.getByRole('button', { name: 'Jane Doe' }));
    expect(onNavigate).toHaveBeenCalledWith('other-1');
  });

  it('marks a name-only relationship as not linked', async () => {
    const relationships: Relationship[] = [
      {
        id: 'rel-0',
        type: 'sibling',
        related_person_id: null,
        related_person_name: 'Unlinked Sibling',
        related_person_deleted: false,
      },
    ];
    mocks.listRelationships.mockResolvedValue({ data: relationships });

    render(<RelationshipsEditor personId="me" />);

    await waitFor(() => expect(screen.getByText('Unlinked Sibling')).toBeInTheDocument());
    expect(screen.getByText('not linked')).toBeInTheDocument();
  });

  it('adds an unlinked relationship when the typed name has no match', async () => {
    mocks.listRelationships.mockResolvedValue({ data: [] });
    mocks.createRelationship.mockResolvedValue({
      id: 'rel-2',
      type: 'parent',
      related_person_name: 'Unlinked Sibling',
      related_person_deleted: false,
    });

    render(<RelationshipsEditor personId="me" />);
    await waitFor(() => expect(screen.getByText(/no relationships yet/i)).toBeInTheDocument());

    await userEvent.type(screen.getByLabelText('related contact'), 'Unlinked Sibling');
    await userEvent.click(screen.getByRole('button', { name: /add relationship/i }));

    await waitFor(() => {
      expect(mocks.createRelationship).toHaveBeenCalledWith('me', {
        type: 'parent',
        related_person_id: undefined,
        related_person_name: 'Unlinked Sibling',
      });
    });
  });

  it('links to a contact selected from the search suggestions', async () => {
    mocks.listRelationships.mockResolvedValue({ data: [] });
    mocks.listPersons.mockResolvedValue(makePersonsResponse([makePerson()]));
    mocks.createRelationship.mockResolvedValue({
      id: 'rel-3',
      type: 'parent',
      related_person_id: 'other-1',
      related_person_name: 'Jane Doe',
      related_person_deleted: false,
    });

    render(<RelationshipsEditor personId="me" />);
    await waitFor(() => expect(screen.getByText(/no relationships yet/i)).toBeInTheDocument());

    await userEvent.type(screen.getByLabelText('related contact'), 'Jane');
    const suggestion = await screen.findByRole('button', { name: 'Jane Doe' }, { timeout: 1000 });
    await userEvent.click(suggestion);
    await userEvent.click(screen.getByRole('button', { name: /add relationship/i }));

    await waitFor(() => {
      expect(mocks.createRelationship).toHaveBeenCalledWith('me', {
        type: 'parent',
        related_person_id: 'other-1',
        related_person_name: undefined,
      });
    });
  });

  it('removes a relationship', async () => {
    const relationships: Relationship[] = [
      {
        id: 'rel-1',
        type: 'child',
        related_person_id: null,
        related_person_name: 'Kid Name',
        related_person_deleted: false,
      },
    ];
    mocks.listRelationships.mockResolvedValue({ data: relationships });
    mocks.deleteRelationship.mockResolvedValue(undefined);

    render(<RelationshipsEditor personId="me" />);
    await waitFor(() => expect(screen.getByText('Kid Name')).toBeInTheDocument());

    await userEvent.click(screen.getByRole('button', { name: /remove relationship to kid name/i }));

    await waitFor(() => {
      expect(mocks.deleteRelationship).toHaveBeenCalledWith('me', 'rel-1');
    });
  });
});
