import { describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { PersonList } from '../src/components/PersonList';
import type { Person } from '../src/types';

function makePerson(overrides: Partial<Person> = {}): Person {
  return {
    id: '1',
    first_name: 'Ada',
    middle_names: [],
    last_name: 'Lovelace',
    display_name: 'Ada Lovelace',
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

describe('PersonList', () => {
  it('renders labeled phone number values, not [object Object]', () => {
    const person = makePerson({
      phone_numbers: [
        { label: 'mobile', value: '(612) 805-3044' },
        { label: 'home', value: '(555) 000-1111' },
      ],
    });

    render(<PersonList persons={[person]} onEdit={vi.fn()} onDelete={vi.fn()} />);

    expect(screen.getByText('(612) 805-3044 · (555) 000-1111')).toBeInTheDocument();
    expect(screen.queryByText(/object Object/)).not.toBeInTheDocument();
  });

  it('toggles favorite state via the star button', async () => {
    const onToggleFavorite = vi.fn();
    const person = makePerson({ is_favorite: false });

    render(
      <PersonList
        persons={[person]}
        onEdit={vi.fn()}
        onDelete={vi.fn()}
        onToggleFavorite={onToggleFavorite}
      />,
    );

    const star = screen.getByRole('button', { name: /favorite ada lovelace/i });
    expect(star).toHaveAttribute('aria-pressed', 'false');
    star.click();
    expect(onToggleFavorite).toHaveBeenCalledWith(person);
  });
});
