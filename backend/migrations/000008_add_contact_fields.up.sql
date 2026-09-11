-- Adds richer contact fields: labeled emails, labeled phone numbers (replacing
-- the plain text array), structured addresses, organization, and free-text
-- notes, so Google contact data (email, phone type, address, company/title,
-- notes) can round-trip without being squeezed into custom_fields.

ALTER TABLE persons ADD COLUMN emails JSONB NOT NULL DEFAULT '[]'::jsonb;
ALTER TABLE persons ADD COLUMN addresses JSONB NOT NULL DEFAULT '[]'::jsonb;
ALTER TABLE persons ADD COLUMN organization JSONB;
ALTER TABLE persons ADD COLUMN notes TEXT;

ALTER TABLE persons ADD COLUMN phone_numbers_new JSONB NOT NULL DEFAULT '[]'::jsonb;
UPDATE persons SET phone_numbers_new = (
    SELECT COALESCE(jsonb_agg(jsonb_build_object('label', '', 'value', pn)), '[]'::jsonb)
    FROM unnest(phone_numbers) AS pn
)
WHERE phone_numbers IS NOT NULL AND array_length(phone_numbers, 1) > 0;
ALTER TABLE persons DROP COLUMN phone_numbers;
ALTER TABLE persons RENAME COLUMN phone_numbers_new TO phone_numbers;
