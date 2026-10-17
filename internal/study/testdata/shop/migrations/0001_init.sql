CREATE TABLE plans (id bigint PRIMARY KEY, quota bigint NOT NULL, price bigint NOT NULL);
CREATE TABLE usage (
  id bigint PRIMARY KEY,
  company_id bigint NOT NULL,
  period text NOT NULL,
  total bigint NOT NULL,
  zero_rated boolean NOT NULL DEFAULT false,
  plan_id bigint REFERENCES plans (id),
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_usage_company_period ON usage (company_id, period);
CREATE TABLE charges (usage_id bigint NOT NULL, amount bigint NOT NULL);
