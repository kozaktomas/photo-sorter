-- Photo full-text search: Czech-aware, complementary to CLIP text-to-image.
--
-- The `fts` column is a generated tsvector over the user-visible text
-- fields of a photo. The `simple` dictionary is chosen on purpose: Czech
-- stemming dictionaries exist but require unpacking and produce too many
-- false-positive matches for short queries. Pair `simple` with
-- `unaccent` to keep stems intact while folding diacritics, so searches
-- for "deti" or "tomas" match "Děti" and "Tomáš" without exposing the
-- caller to dictionary quirks.
--
-- Field weights (A > B > C > D) bias the ranker toward titles, then
-- descriptions, then notes, with file_name as a last-resort tiebreaker.

-- The default unaccent(text) function is STABLE (its result depends on
-- search_path, since the dictionary is resolved by unqualified name). A
-- generated column expression must be IMMUTABLE, so we wrap unaccent in
-- an IMMUTABLE shim that pins the dictionary by its fully-qualified
-- name.
CREATE OR REPLACE FUNCTION immutable_unaccent(text) RETURNS text
    LANGUAGE sql IMMUTABLE PARALLEL SAFE STRICT AS
$$ SELECT public.unaccent('public.unaccent', $1) $$;

ALTER TABLE photos ADD COLUMN fts tsvector
    GENERATED ALWAYS AS (
        setweight(to_tsvector('simple', immutable_unaccent(coalesce(title, ''))), 'A') ||
        setweight(to_tsvector('simple', immutable_unaccent(coalesce(description, ''))), 'B') ||
        setweight(to_tsvector('simple', immutable_unaccent(coalesce(notes, ''))), 'C') ||
        -- file_name needs its punctuation stripped first: the default parser
        -- treats `older.jpg` as a single `host` token (domain-name pattern),
        -- so a search for `older` would otherwise miss the row. Collapsing
        -- non-alphanumerics into spaces lets us index `older` and `jpg`
        -- separately the way a user expects.
        setweight(to_tsvector('simple',
            regexp_replace(immutable_unaccent(coalesce(file_name, '')), '[^a-zA-Z0-9]+', ' ', 'g')
        ), 'D')
    ) STORED;

CREATE INDEX photos_fts_idx ON photos USING GIN (fts);
