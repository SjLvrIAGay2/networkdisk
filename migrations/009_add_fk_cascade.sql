SET @fk_shares_owner = (
    SELECT CONSTRAINT_NAME FROM INFORMATION_SCHEMA.KEY_COLUMN_USAGE
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'shares'
    AND COLUMN_NAME = 'owner_id' AND REFERENCED_TABLE_NAME = 'users'
);
SET @sql_drop_shares = CONCAT('ALTER TABLE shares DROP FOREIGN KEY ', @fk_shares_owner);
PREPARE stmt_shares_drop FROM @sql_drop_shares;
EXECUTE stmt_shares_drop;
DEALLOCATE PREPARE stmt_shares_drop;
ALTER TABLE shares ADD FOREIGN KEY (owner_id) REFERENCES users(id) ON DELETE CASCADE;

SET @fk_td_user = (
    SELECT CONSTRAINT_NAME FROM INFORMATION_SCHEMA.KEY_COLUMN_USAGE
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'temp_downloads'
    AND COLUMN_NAME = 'user_id' AND REFERENCED_TABLE_NAME = 'users'
);
SET @sql_drop_td = CONCAT('ALTER TABLE temp_downloads DROP FOREIGN KEY ', @fk_td_user);
PREPARE stmt_td_drop FROM @sql_drop_td;
EXECUTE stmt_td_drop;
DEALLOCATE PREPARE stmt_td_drop;
ALTER TABLE temp_downloads ADD FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE;
