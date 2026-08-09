package store

import "database/sql"

// GetResourceMediaDecision derives a resource decision from the canonical
// media group. It replaces the duplicated media_matches resource table.
func (s *sqlStore) GetResourceMediaDecision(resourceID int64) (ResourceMediaDecision, bool, error) {
	var decision ResourceMediaDecision
	var groupStatus, decisionSource string
	err := s.db.QueryRow(`SELECT m.resource_id, g.selected_source, g.selected_id, g.canonical_title,
		COALESCE(c.year, g.year), g.confidence, g.status, g.decision_source, g.reason, g.updated_at
		FROM media_group_members m
		JOIN media_groups g ON g.id=m.group_id
		LEFT JOIN media_candidates c ON c.group_id=g.id AND c.source=g.selected_source AND c.provider_id=g.selected_id
		WHERE m.resource_id=?`, resourceID).Scan(
		&decision.ResourceID, &decision.Source, &decision.ProviderID, &decision.Title,
		&decision.Year, &decision.Confidence, &groupStatus, &decisionSource, &decision.Reason, &decision.UpdatedAt)
	if err == sql.ErrNoRows {
		return ResourceMediaDecision{ResourceID: resourceID}, false, nil
	}
	if err != nil {
		return decision, false, err
	}
	switch groupStatus {
	case "verified", "published":
		if decisionSource == "manual" {
			decision.Status = "confirmed"
		} else {
			decision.Status = "auto"
		}
	default:
		decision.Status = "pending"
	}
	return decision, true, nil
}
