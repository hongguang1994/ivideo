package store

import (
	"database/sql"
	"fmt"
	"time"
)

// cleanupLegacySchema preserves old manual decisions before removing tables
// superseded by the media-group pipeline. It is safe to run on every startup.
func cleanupLegacySchema(db *sql.DB, d dialect) error {
	matchesExist, err := legacyTableExists(db, d, "media_matches")
	if err != nil {
		return fmt.Errorf("检查旧媒体匹配表: %w", err)
	}
	classificationsExist, err := legacyTableExists(db, d, "media_classifications")
	if err != nil {
		return fmt.Errorf("检查旧媒体分类表: %w", err)
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("开始清理旧表: %w", err)
	}
	defer tx.Rollback()

	if matchesExist {
		type legacyDecision struct {
			groupID                   int64
			source, providerID, title string
			reason, library           string
			year, confidence          int
		}
		libraryColumn := "''"
		classificationJoin := ""
		if classificationsExist {
			libraryColumn = "COALESCE(mc.library, '')"
			classificationJoin = " LEFT JOIN media_classifications mc ON mc.resource_id=mm.resource_id"
		}
		query := `SELECT gm.group_id, mm.source, mm.provider_id, mm.title, mm.year,
			mm.confidence, mm.reason, ` + libraryColumn + `
			FROM media_matches mm
			JOIN media_group_members gm ON gm.resource_id=mm.resource_id` + classificationJoin + `
			WHERE mm.status='confirmed' AND mm.provider_id<>''
			ORDER BY mm.confidence DESC, mm.updated_at DESC`
		rows, err := tx.Query(query)
		if err != nil {
			return fmt.Errorf("读取旧人工匹配: %w", err)
		}
		seen := map[int64]bool{}
		decisions := make([]legacyDecision, 0)
		for rows.Next() {
			var decision legacyDecision
			if err := rows.Scan(&decision.groupID, &decision.source, &decision.providerID, &decision.title,
				&decision.year, &decision.confidence, &decision.reason, &decision.library); err != nil {
				rows.Close()
				return fmt.Errorf("解析旧人工匹配: %w", err)
			}
			if seen[decision.groupID] {
				continue
			}
			seen[decision.groupID] = true
			decisions = append(decisions, decision)
		}
		if err := rows.Close(); err != nil {
			return err
		}
		if err := rows.Err(); err != nil {
			return err
		}
		for _, decision := range decisions {
			if decision.library == "" {
				decision.library = "review"
			}
			if _, err := tx.Exec(`UPDATE media_groups SET status='verified', decision_source='manual',
				selected_source=?, selected_id=?, canonical_title=?, suggested_library=?, year=?,
				confidence=?, reason=?, updated_at=? WHERE id=? AND decision_source<>'manual'`,
				decision.source, decision.providerID, decision.title, decision.library, decision.year,
				decision.confidence, decision.reason, time.Now().Unix(), decision.groupID); err != nil {
				return fmt.Errorf("迁移旧人工匹配: %w", err)
			}
		}
	}

	now := time.Now().Unix()
	if _, err := tx.Exec(`UPDATE media_processing_jobs SET status='failed', error='服务重启，任务已中断',
		finished_at=?, updated_at=? WHERE status='running'`, now, now); err != nil {
		return fmt.Errorf("结束遗留媒体任务: %w", err)
	}
	for _, table := range []string{"media_matches", "media_classifications", "import_jobs"} {
		if _, err := tx.Exec(`DROP TABLE IF EXISTS ` + table); err != nil {
			return fmt.Errorf("删除旧表 %s: %w", table, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交旧表清理: %w", err)
	}
	return nil
}

func legacyTableExists(db *sql.DB, d dialect, table string) (bool, error) {
	var count int
	if d.driver == "mysql" {
		err := db.QueryRow(`SELECT COUNT(*) FROM information_schema.tables
			WHERE table_schema=DATABASE() AND table_name=?`, table).Scan(&count)
		return count > 0, err
	}
	err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&count)
	return count > 0, err
}
