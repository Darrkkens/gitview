-- Overage charges are looked up by usage.
CREATE UNIQUE INDEX IF NOT EXISTS idx_charges_usage ON charges USING btree (usage_id);
