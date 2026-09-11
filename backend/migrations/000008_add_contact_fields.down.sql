ALTER TABLE persons ADD COLUMN phone_numbers_old TEXT[] NOT NULL DEFAULT '{}';
UPDATE persons SET phone_numbers_old = (
    SELECT COALESCE(array_agg(entry ->> 'value'), '{}')
    FROM jsonb_array_elements(phone_numbers) AS entry
)
WHERE phone_numbers IS NOT NULL AND jsonb_array_length(phone_numbers) > 0;
ALTER TABLE persons DROP COLUMN phone_numbers;
ALTER TABLE persons RENAME COLUMN phone_numbers_old TO phone_numbers;

ALTER TABLE persons DROP COLUMN IF EXISTS notes;
ALTER TABLE persons DROP COLUMN IF EXISTS organization;
ALTER TABLE persons DROP COLUMN IF EXISTS addresses;
ALTER TABLE persons DROP COLUMN IF EXISTS emails;
