-- +goose Up
INSERT INTO users (email, password_hash, role, email_verified, trust_score, balance)
VALUES ('admin@example.com', '$2a$10$jy1lBEoFJAtuAVRqknaIGOuO0NTRcpCcQm6EcaP1Bm5o3DdOXXfoC', 'ADMIN', true, 1000, 0)
ON CONFLICT (email) DO NOTHING;

-- +goose Down
DELETE FROM users WHERE email = 'admin@example.com';
