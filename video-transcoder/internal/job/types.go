package job

type TranscodeJob struct {
	VideoID      string   `json:"video_id"`
	UserID       int64    `json:"user_id"`
	SourceBucket string   `json:"source_bucket"`
	SourceKey    string   `json:"source_key"`
	OutputPrefix string   `json:"output_prefix"`
	Resolutions  []string `json:"resolutions"`
	Formats      []string `json:"formats"`
}

type OutputFile struct {
	Resolution string `json:"resolution"`
	Format     string `json:"format"`
	Bucket     string `json:"bucket"`
	Key        string `json:"key"`
	SizeBytes  int64  `json:"size_bytes"`
}

type CompletionMessage struct {
	VideoID      string       `json:"video_id"`
	UserID       int64        `json:"user_id"`
	SourceBucket string       `json:"source_bucket,omitempty"`
	SourceKey    string       `json:"source_key,omitempty"`
	Status       string       `json:"status"`
	Error        string       `json:"error,omitempty"`
	Outputs      []OutputFile `json:"outputs,omitempty"`
	Processed    string       `json:"processed_at"`
}
