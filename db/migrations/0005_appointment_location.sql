-- 0005_appointment_location.sql
-- An optional address/location for an appointment (clinic name, street address, or
-- a room). Free text — rendered with a maps link in the UI.

ALTER TABLE appointments ADD COLUMN location TEXT;
