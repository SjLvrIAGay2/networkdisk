CREATE TABLE files (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    user_id BIGINT NOT NULL,
    parent_id BIGINT NULL,
    name VARCHAR(255) NOT NULL,
    is_dir TINYINT(1) NOT NULL DEFAULT 0,
    size BIGINT NOT NULL DEFAULT 0,
    file_hash VARCHAR(64) NOT NULL DEFAULT '',
    storage_key VARCHAR(64) NOT NULL DEFAULT '',
    thumbnail_key VARCHAR(64) NOT NULL DEFAULT '',
    mime_type VARCHAR(255) NOT NULL DEFAULT '',
    is_deleted TINYINT(1) NOT NULL DEFAULT 0,
    deleted_at TIMESTAMP NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id),
    FOREIGN KEY (parent_id) REFERENCES files(id),
    INDEX idx_parent_dir (parent_id, is_deleted, name),
    INDEX idx_file_hash (file_hash),
    INDEX idx_storage_key (storage_key),
    INDEX idx_user_deleted (user_id, is_deleted)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

ALTER TABLE users ADD COLUMN storage_used BIGINT NOT NULL DEFAULT 0;
