CREATE UNLOGGED TABLE payments (
	correlationId UUID PRIMARY KEY,
	amount DECIMAL NOT NULL,
	gateway_type int NOT NULL,
	requested_at TIMESTAMP NOT NULL
);

CREATE INDEX payments_requested_at ON payments (requested_at);