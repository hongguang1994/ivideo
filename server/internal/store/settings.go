package store

import (
	"database/sql"
	"time"
)

// GetSetting 读取一个应用设置；不存在时 found=false。
func (s *sqlStore) GetSetting(key string) (value string, found bool, err error) {
	err = s.db.QueryRow(`SELECT setting_value FROM app_settings WHERE setting_key = ?`, key).Scan(&value)
	if err == nil {
		return value, true, nil
	}
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	return "", false, err
}

// SetSetting 使用当前数据库方言的 upsert 保存应用设置。
func (s *sqlStore) SetSetting(key, value string) error {
	now := time.Now().Unix()
	if s.d.driver == "mysql" {
		_, err := s.db.Exec(`INSERT INTO app_settings (setting_key, setting_value, updated_at) VALUES (?, ?, ?)
			ON DUPLICATE KEY UPDATE setting_value = VALUES(setting_value), updated_at = VALUES(updated_at)`, key, value, now)
		return err
	}
	_, err := s.db.Exec(`INSERT INTO app_settings (setting_key, setting_value, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(setting_key) DO UPDATE SET setting_value = excluded.setting_value, updated_at = excluded.updated_at`, key, value, now)
	return err
}
