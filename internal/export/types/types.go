package types

// ExportRecord represents a record in the export file.
type ExportRecord struct {
	Hash       string
	Status     string
	Reason     string
	Confidence float64
}
