-- Schema of the fixture: clients belong to a company.
CREATE TABLE companies (
    id uuid PRIMARY KEY,
    name text NOT NULL
);

CREATE TABLE clients (
    id uuid PRIMARY KEY,
    name text NOT NULL,
    password_hash text,
    CONSTRAINT clients_name_key UNIQUE (name)
);

ALTER TABLE clients ADD COLUMN company_id uuid REFERENCES companies (id);

CREATE TABLE reports (
    id int PRIMARY KEY,
    body text
);
