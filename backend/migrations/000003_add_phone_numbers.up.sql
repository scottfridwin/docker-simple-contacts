ALTER TABLE persons
    ADD COLUMN phone_numbers TEXT[] NOT NULL DEFAULT '{}';
