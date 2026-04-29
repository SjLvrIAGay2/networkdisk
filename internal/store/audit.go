package store

import (
	"context"
	"fmt"
	"time"

	"networkdisk/internal/model"
)

func (s *Store) CreateAuditLog(log *model.AuditLog) error {
	if log.UserID <= 0 {
		return fmt.Errorf("create audit log: user id must be positive")
	}
	if log.Action == "" {
		return fmt.Errorf("create audit log: action must not be empty")
	}
	_, err := s.DB.ExecContext(context.Background(),
		"INSERT INTO audit_logs (user_id, action, target_type, target_id, detail, ip) VALUES (?, ?, ?, ?, ?, ?)",
		log.UserID, log.Action, log.TargetType, log.TargetID, log.Detail, log.IP,
	)
	if err != nil {
		return fmt.Errorf("create audit log: %w", err)
	}
	return nil
}

var validAuditActions = map[string]bool{
	"upload": true, "download": true, "delete": true, "permanent_delete": true,
	"share_create": true, "share_delete": true, "share_download": true, "share_verify": true,
	"register": true, "login": true, "totp_enable": true, "totp_disable": true,
}

func (s *Store) AuditLogsByUser(userID int64, action string, limit, offset int) ([]*model.AuditLog, error) {
	if userID <= 0 {
		return nil, fmt.Errorf("audit logs by user: user id must be positive")
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	query := "SELECT id, user_id, action, target_type, target_id, detail, ip, created_at FROM audit_logs WHERE user_id = ?"
	args := []interface{}{userID}
	if action != "" {
		if !validAuditActions[action] {
			return nil, fmt.Errorf("audit logs by user: invalid action %q", action)
		}
		query += " AND action = ?"
		args = append(args, action)
	}
	query += " ORDER BY created_at DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := s.DB.QueryContext(context.Background(), query, args...)
	if err != nil {
		return nil, fmt.Errorf("audit logs by user: %w", err)
	}
	defer rows.Close()
	var logs []*model.AuditLog
	for rows.Next() {
		l := &model.AuditLog{}
		if err := rows.Scan(&l.ID, &l.UserID, &l.Action, &l.TargetType, &l.TargetID, &l.Detail, &l.IP, &l.CreatedAt); err != nil {
			return nil, fmt.Errorf("audit logs scan: %w", err)
		}
		logs = append(logs, l)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("audit logs iterate: %w", err)
	}
	return logs, nil
}

func (s *Store) DeleteExpiredAuditLogs(retentionDays int) (int64, error) {
	cutoff := time.Now().AddDate(0, 0, -retentionDays)
	result, err := s.DB.ExecContext(context.Background(), "DELETE FROM audit_logs WHERE created_at < ?", cutoff)
	if err != nil {
		return 0, fmt.Errorf("delete expired audit logs: %w", err)
	}
	return result.RowsAffected()
}
