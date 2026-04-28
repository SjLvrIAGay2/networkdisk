SET @col_exists = (SELECT COUNT(*) FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'users' AND COLUMN_NAME = 'totp_secret');
SET @sql_add_totp = IF(@col_exists = 0, 'ALTER TABLE users ADD COLUMN totp_secret VARCHAR(64) NOT NULL DEFAULT ''''', 'SELECT 1');
PREPARE stmt_totp FROM @sql_add_totp;
EXECUTE stmt_totp;
DEALLOCATE PREPARE stmt_totp;

SET @idx_exists = (SELECT COUNT(*) FROM INFORMATION_SCHEMA.STATISTICS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'files' AND INDEX_NAME = 'idx_f_name_ft');
SET @sql_add_ft = IF(@idx_exists = 0, 'ALTER TABLE files ADD FULLTEXT INDEX idx_f_name_ft (name) WITH PARSER ngram', 'SELECT 1');
PREPARE stmt_ft FROM @sql_add_ft;
EXECUTE stmt_ft;
DEALLOCATE PREPARE stmt_ft;
