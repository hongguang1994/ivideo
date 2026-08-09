package store

// ResourceRepository owns imported media resources.
type ResourceRepository interface {
	AddResource(r Resource) (int64, error)
	ListResources() ([]Resource, error)
	GetResource(id int64) (Resource, error)
	CountResources() (int, error)
}

// CacheRepository owns the playback cache state machine.
type CacheRepository interface {
	GetCacheItem(resourceID int64) (CacheItem, error)
	SetTransferring(resourceID int64, backend string) error
	SetFailed(resourceID int64, backend, errMsg string, failCount int, nextRetryAt int64) error
	SetReady(resourceID int64, backend, cachePath, directURL string, size int64) error
	TouchAccess(resourceID int64) error
	MarkCleaned(resourceID int64) error
	ListReady() ([]CacheItem, error)
}

// CredentialRepository owns external provider credentials.
type CredentialRepository interface {
	GetCredential(provider string) (Credential, bool, error)
	SetCredential(provider, token, extra string) error
	SetCredentialToken(provider, token string) error
	ListCredentialProviders() (map[string]bool, error)
}

// SettingsRepository owns persisted application settings.
type SettingsRepository interface {
	GetSetting(key string) (string, bool, error)
	SetSetting(key, value string) error
}

// MediaRepository owns identification, review and publication state.
type MediaRepository interface {
	GetResourceMediaDecision(resourceID int64) (ResourceMediaDecision, bool, error)
	GetContentAnalysis(resourceID int64) (ContentAnalysis, bool, error)
	SetContentAnalysis(analysis ContentAnalysis) error
	FindContentAnalysisBySignature(signature string, excludeResourceID int64) (ContentAnalysis, bool, error)
	ListContentAnalyses(excludeResourceID int64) ([]ContentAnalysis, error)
	SyncMediaGroup(group MediaGroup, members []MediaGroupMember) (int64, error)
	DeleteOrphanMediaGroups() (int64, error)
	SetMediaGroupDecision(groupID int64, decision MediaGroupDecision) error
	ReplaceMediaCandidates(groupID int64, candidates []MediaCandidate) error
	ListMediaGroupDetails(status string, limit, offset int) ([]MediaGroupDetail, int, error)
	GetMediaGroupDetail(groupID int64) (MediaGroupDetail, error)
	GetResourceGroupStates() (map[int64]MediaGroupState, error)
	RecordMediaPublication(publication MediaPublication) error
	SaveMediaPathAnalysis(analysis MediaPathAnalysis, candidates []MediaTitleCandidate) error
	GetMediaPathAnalysis(resourceID int64) (MediaPathAnalysis, []MediaTitleCandidate, bool, error)
	RecordMediaVerification(verification MediaVerification) error
	ListMediaVerifications(groupID int64) ([]MediaVerification, error)
	RecordMediaPublicationArtifact(artifact MediaPublicationArtifact) error
	ReplaceMediaGroupTags(groupID int64, tags []MediaTag) error
	ListMediaGroupTags(groupID int64) ([]MediaTag, error)
	FindMediaAliases(alias, mediaKind string, year int) ([]MediaAlias, error)
	UpsertMediaAlias(alias MediaAlias) error
}

// JobRepository owns long-running media processing jobs and their steps.
type JobRepository interface {
	StartMediaProcessingJob(job MediaProcessingJob) (int64, error)
	UpdateMediaProcessingJob(job MediaProcessingJob) error
	RecordMediaProcessingStep(step MediaProcessingStep) error
}

// ShareRepository owns collected public share links and their health state.
type ShareRepository interface {
	AddShare(s Share) (int64, error)
	SyncShares(shares []Share) (added, existing int, err error)
	ListShares() ([]Share, error)
	GetShare(id int64) (Share, error)
	UpdateShare(s Share) error
	DeleteShare(id int64) error
}
