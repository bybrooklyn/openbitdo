package protocol

// ResponseStatus classifies a validated device response.
type ResponseStatus string

const (
	StatusOk        ResponseStatus = "Ok"
	StatusInvalid   ResponseStatus = "Invalid"
	StatusMalformed ResponseStatus = "Malformed"
)

// ResponseFrame is a validated, parsed device response.
type ResponseFrame struct {
	Raw          []byte
	Status       ResponseStatus
	ParsedFields map[string]uint32
}
