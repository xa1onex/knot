-- Stage 7.5 — per-route TLS mode (edge_terminate | origin_tls)
ALTER TABLE edge_routes ADD COLUMN tls_mode TEXT NOT NULL DEFAULT 'edge_terminate';
