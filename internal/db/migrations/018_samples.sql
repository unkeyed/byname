-- A check can ask each question more than once on each pair, so one
-- answer that flips is not read as a move. sample numbers a cell's
-- answers from 1. Every answer stored before this is sample 1, and
-- samples defaults to 1, so an upgrade asks no more than before.
ALTER TABLE answer ADD COLUMN sample smallint NOT NULL DEFAULT 1 CHECK (sample >= 1);
ALTER TABLE answer DROP CONSTRAINT answer_run_id_question_id_surface_model_id_key;
ALTER TABLE answer ADD CONSTRAINT answer_cell_sample UNIQUE (run_id, question_id, surface_model_id, sample);

ALTER TABLE instance_settings ADD COLUMN samples int NOT NULL DEFAULT 1 CHECK (samples BETWEEN 1 AND 5);
