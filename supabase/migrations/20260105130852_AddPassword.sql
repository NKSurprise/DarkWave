ALTER TABLE clients
ADD COLUMN if not exists password TEXT NOT NULL DEFAULT '';
