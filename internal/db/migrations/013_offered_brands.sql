-- Whether an answer offered a brand it named as a choice for its question,
-- or named it for another reason, such as a tool, a host or an example.
-- NULL for answers read before Byname asked, which count as offered. Only
-- offered brands are listed as possible competitors, and only once answers
-- to at least other_brands_min_questions different questions offered them.
ALTER TABLE discovered_mention ADD COLUMN offered boolean;

ALTER TABLE instance_settings
    ADD COLUMN other_brands_min_questions int NOT NULL DEFAULT 3
        CHECK (other_brands_min_questions BETWEEN 1 AND 50);
