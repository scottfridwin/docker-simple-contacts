-- Adds free-form labels (CATEGORIES-style tags, e.g. Google Contacts group
-- names), stored per person as a simple string array - no separate label
-- entity, matching the vCard/CardDAV CATEGORIES model rather than Google's
-- heavier ID-referenced contactGroups resource.
ALTER TABLE persons ADD COLUMN labels TEXT[] NOT NULL DEFAULT '{}';
