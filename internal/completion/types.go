package completion

type Message struct {
	VideoID         string       `json:"video_id"`
	UserID          int64        `json:"user_id"`
	SourceBucket    string       `json:"source_bucket,omitempty"`
	SourceKey       string       `json:"source_key,omitempty"`
	Status          string       `json:"status"`
	Error           string       `json:"error,omitempty"`
	DurationSeconds int64        `json:"duration_seconds,omitempty"`
	ThumbnailURL    string       `json:"thumbnail_url,omitempty"`
	Outputs         []OutputFile `json:"outputs,omitempty"`
	ProcessedAt     string       `json:"processed_at"`
}

type OutputFile struct {
	Resolution string `json:"resolution"`
	Format     string `json:"format"`
	Bucket     string `json:"bucket"`
	Key        string `json:"key"`
	SizeBytes  int64  `json:"size_bytes"`
}
