package store

import (
	"strings"
	"time"
	"unicode"
)

func normalizeMediaAlias(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, strings.TrimSpace(value))
}

func (s *sqlStore) FindMediaAliases(alias, mediaKind string, year int) ([]MediaAlias, error) {
	normalized := normalizeMediaAlias(alias)
	if normalized == "" {
		return []MediaAlias{}, nil
	}
	rows, err := s.db.Query(`SELECT id, alias, normalized_alias, canonical_title, media_kind, source,
		provider_id, year, confidence, hit_count, created_at, updated_at
		FROM media_aliases WHERE normalized_alias = ? AND media_kind = ? AND (year = 0 OR ? = 0 OR year = ?)
		ORDER BY confidence DESC, hit_count DESC, updated_at DESC`, normalized, mediaKind, year, year)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MediaAlias{}
	for rows.Next() {
		var item MediaAlias
		if err := rows.Scan(&item.ID, &item.Alias, &item.NormalizedAlias, &item.CanonicalTitle,
			&item.MediaKind, &item.Source, &item.ProviderID, &item.Year, &item.Confidence,
			&item.HitCount, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *sqlStore) UpsertMediaAlias(item MediaAlias) error {
	item.Alias = strings.TrimSpace(item.Alias)
	item.NormalizedAlias = normalizeMediaAlias(item.Alias)
	if item.NormalizedAlias == "" || item.CanonicalTitle == "" || item.MediaKind == "" || item.Source == "" || item.ProviderID == "" {
		return nil
	}
	now := time.Now().Unix()
	if s.d.driver == "mysql" {
		_, err := s.db.Exec(`INSERT INTO media_aliases (alias, normalized_alias, canonical_title, media_kind, source, provider_id, year, confidence, hit_count, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?)
			ON DUPLICATE KEY UPDATE alias=VALUES(alias), canonical_title=VALUES(canonical_title), confidence=GREATEST(confidence, VALUES(confidence)), hit_count=hit_count+1, updated_at=VALUES(updated_at)`,
			item.Alias, item.NormalizedAlias, item.CanonicalTitle, item.MediaKind, item.Source, item.ProviderID, item.Year, item.Confidence, now, now)
		return err
	}
	_, err := s.db.Exec(`INSERT INTO media_aliases (alias, normalized_alias, canonical_title, media_kind, source, provider_id, year, confidence, hit_count, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?)
		ON CONFLICT(normalized_alias, media_kind, year, source, provider_id) DO UPDATE SET alias=excluded.alias, canonical_title=excluded.canonical_title, confidence=MAX(confidence, excluded.confidence), hit_count=hit_count+1, updated_at=excluded.updated_at`,
		item.Alias, item.NormalizedAlias, item.CanonicalTitle, item.MediaKind, item.Source, item.ProviderID, item.Year, item.Confidence, now, now)
	return err
}
