package loki

// lokiPushRequest represents the JSON payload sent to Loki.
type lokiPushRequest struct {
	Streams []stream `json:"streams"`
}

// stream represents a log stream with labels and values.
type stream struct {
	Stream map[string]string `json:"stream"`
	Values []streamValue     `json:"values"`
}

// streamValue is a tuple of [timestamp, log_line].
type streamValue []string

// logEntry represents a structured log entry from Zap.
type logEntry struct {
	Level     string         `json:"level"`
	Timestamp float64        `json:"ts"`
	Message   string         `json:"msg"`
	Caller    string         `json:"caller"`
	Stack     string         `json:"stacktrace,omitempty"`
	Fields    map[string]any `json:",inline"`
	raw       string         // Original JSON string
}
