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

    await userEvent.click(screen.getByRole('button', { name: /add middle names/i }));
    await userEvent.type(screen.getByLabelText('Middle names 1'), 'A');
    await userEvent.click(screen.getByRole('button', { name: /add middle names/i }));
    await userEvent.type(screen.getByLabelText('Middle names 2'), 'B');

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
      emails: [],
      phone_numbers: [],
      addresses: [],
      organization: { name: '', title: '', department: '' },
      notes: '',
      middle_names: ['A', 'B'],
      custom_fields: { blood_type: 'O+' },
      sync_metadata: {},
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

  it('submits labeled emails, phone numbers, addresses, organization, and notes', async () => {
    const onSubmit = vi.fn();
    render(<PersonForm onSubmit={onSubmit} onCancel={() => {}} />);

    await userEvent.type(screen.getByLabelText(/first name/i), 'Scott');
    await userEvent.type(screen.getByLabelText(/last name/i), 'Fridlund');

    await userEvent.click(screen.getByRole('button', { name: /add emails/i }));
    await userEvent.type(screen.getByLabelText('Emails 1 label'), 'work');
    await userEvent.type(screen.getByLabelText('Emails 1'), 'scott@example.com');

    await userEvent.click(screen.getByRole('button', { name: /add phone numbers/i }));
    await userEvent.type(screen.getByLabelText('Phone numbers 1 label'), 'mobile');
    await userEvent.type(screen.getByLabelText('Phone numbers 1'), '+1-555-0100');

    await userEvent.click(screen.getByRole('button', { name: /add address/i }));
    await userEvent.type(screen.getByLabelText('address 1 city'), 'Springfield');

    await userEvent.type(screen.getByLabelText(/company/i), 'Acme');
    await userEvent.type(screen.getByLabelText(/^title$/i), 'Engineer');
    await userEvent.type(screen.getByLabelText(/notes/i), 'Met at a conference.');

    await userEvent.click(screen.getByRole('button', { name: /save/i }));

    expect(onSubmit).toHaveBeenCalledTimes(1);
    const submitted = onSubmit.mock.calls[0][0];
    expect(submitted.emails).toEqual([{ label: 'work', value: 'scott@example.com' }]);
    expect(submitted.phone_numbers).toEqual([{ label: 'mobile', value: '+1-555-0100' }]);
    expect(submitted.addresses).toEqual([
      { label: '', street: '', city: 'Springfield', region: '', postal_code: '', country: '' },
    ]);
    expect(submitted.organization).toEqual({ name: 'Acme', title: 'Engineer', department: '' });
    expect(submitted.notes).toBe('Met at a conference.');
  });
});
