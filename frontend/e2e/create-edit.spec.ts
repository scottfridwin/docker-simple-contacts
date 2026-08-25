import { expect, test } from '@playwright/test';

// Covers the primary create -> edit flow required by the acceptance criteria.
test('create and edit a contact', async ({ page }) => {
  const unique = Date.now();
  const firstName = `Test${unique}`;

  await page.goto('/');

  await page.getByRole('button', { name: /add contact/i }).click();

  await page.getByLabel(/first name/i).fill(firstName);
  await page.getByLabel(/last name/i).fill('Person');
  await page.getByRole('button', { name: /add custom field/i }).click();
  await page.getByLabel('custom field key 0').fill('blood_type');
  await page.getByLabel('custom field value 0').fill('O+');
  await page.getByRole('button', { name: /^save$/i }).click();

  // The new contact appears in the list.
  const item = page.locator('.person-item', { hasText: `${firstName} Person` });
  await expect(item).toBeVisible();

  // Edit the contact's last name.
  await item.getByRole('button', { name: /edit/i }).click();
  await page.getByLabel(/last name/i).fill('Edited');
  await page.getByRole('button', { name: /^save$/i }).click();

  await expect(page.locator('.person-item', { hasText: `${firstName} Edited` })).toBeVisible();
});
