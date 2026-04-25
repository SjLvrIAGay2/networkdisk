CREATE TABLE IF NOT EXISTS schema_versions (
    version     INT          NOT NULL PRIMARY KEY,
    filename    VARCHAR(255) NOT NULL,
    applied_at  TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP,
    checksum    VARCHAR(64)  NOT NULL,
    UNIQUE KEY uq_filename (filename)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
