-- +goose Up

-- One-tap sentiment captured at high-engagement moments (e.g. right after a
-- drawing is submitted). rating: 1 = negative, 2 = neutral, 3 = positive.
-- NULL when the row is a plain written message from the /feedback page.
-- context tags where it came from (e.g. 'post_upload') so the admin inbox
-- can tell a quick reaction apart from a typed note.
ALTER TABLE feedback ADD COLUMN rating  SMALLINT;
ALTER TABLE feedback ADD COLUMN context TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE feedback DROP COLUMN IF EXISTS context;
ALTER TABLE feedback DROP COLUMN IF EXISTS rating;
