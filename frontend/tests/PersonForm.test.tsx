import { describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { PersonForm } from '../src/components/PersonForm';

describe('PersonForm', () => {
  it('requires first and last name', async () => {
    const onSubmit = vi.fn();
    render(<PersonForm onSubmit={onSubmit} onCancel={() => {}} />);

    await userEvent.click(screen.getByRole('button', { name: /save/i }));

    expect(onSubmit).not.toHaveBeenCalled();
    expect(screen.getByText(/first name is required/i)).toBeInTheDocument();
    expect(screen.getByText(/last name is required/i)).toBeInTheDocument();
  });

  it('submits valid values including custom fields', async () => {
    const onSubmit = vi.fn();
    render(<PersonForm onSubmit={onSubmit} onCancel={() => {}} />);

    await userEvent.type(screen.getByLabelText(/first name/i), 'Scott');
    await userEvent.type(screen.getByLabelText(/last name/i), 'Fridlund');
    await userEvent.type(screen.getByLabelText(/middle names/i), 'A, B');

    await userEvent.click(screen.getByRole('button', { name: /add custom field/i }));
    await userEvent.type(screen.getByLabelText('custom field key 0'), 'blood_type');
    await userEvent.type(screen.getByLabelText('custom field value 0'), 'O+');

    await userEvent.click(screen.getByRole('button', { name: /save/i }));

    expect(onSubmit).toHaveBeenCalledTimes(1);
    expect(onSubmit).toHaveBeenCalledWith({
      first_name: 'Scott',
      last_name: 'Fridlund',
      nickname: '',
      pronouns: '',
      birthdate: '',
      phone_numbers: [],
      middle_names: ['A', 'B'],
      custom_fields: { blood_type: 'O+' },
    });
  });

  it('blocks submit on invalid custom field key', async () => {
    const onSubmit = vi.fn();
    render(<PersonForm onSubmit={onSubmit} onCancel={() => {}} />);

    await userEvent.type(screen.getByLabelText(/first name/i), 'A');
    await userEvent.type(screen.getByLabelText(/last name/i), 'B');
    await userEvent.click(screen.getByRole('button', { name: /add custom field/i }));
    await userEvent.type(screen.getByLabelText('custom field key 0'), 'Bad-Key');
    await userEvent.type(screen.getByLabelText('custom field value 0'), 'x');
    await userEvent.click(screen.getByRole('button', { name: /save/i }));

    expect(onSubmit).not.toHaveBeenCalled();
    expect(screen.getByText(/snake_case/i)).toBeInTheDocument();
  });
});
