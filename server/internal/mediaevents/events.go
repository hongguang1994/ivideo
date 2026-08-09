// Package mediaevents contains contracts shared by media business modules.
package mediaevents

const (
	ResourceImportedName = "media.resource.imported"
	MetadataVerifiedName = "media.metadata.verified"
	MediaPublishedName   = "media.published"
)

type ResourceImported struct {
	Trigger  string
	Provider string
	ShareURL string
	Added    int
}

func (ResourceImported) Name() string { return ResourceImportedName }

type MetadataVerified struct {
	Trigger    string
	ImagePaths []string
}

func (MetadataVerified) Name() string { return MetadataVerifiedName }

type MediaPublished struct {
	Trigger      string
	Total        int
	Written      int
	Removed      int
	Unchanged    int
	Deduplicated int
	Conflicts    int
}

func (MediaPublished) Name() string { return MediaPublishedName }
