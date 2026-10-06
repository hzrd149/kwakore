package backend

// ServiceVersion is the public installed manifest version.
type ServiceVersion struct {
	EventID      string `json:"event_id"`
	CreatedAt    int64  `json:"created_at"`
	ArtifactHash string `json:"artifact_hash"`
}

// ServiceDescriptor contains only catalog fields safe for local clients.
type ServiceDescriptor struct {
	Address   string         `json:"address"`
	Name      string         `json:"name"`
	Format    string         `json:"format"`
	Available bool           `json:"available"`
	Version   ServiceVersion `json:"version"`
}

type ServicePage struct {
	Items      []ServiceDescriptor `json:"items"`
	Total      int                 `json:"total"`
	NextOffset *int                `json:"next_offset"`
}

func ServiceInstalled(offset, limit int) ServicePage {
	return ServicePage{Items: []ServiceDescriptor{}}
}
