package parser

// ParseError reports a parsing error with a message and a span in raw source offsets [start, end).
type ParseError struct {
	Msg   string
	Start int
	End   int
}

func (e *ParseError) Error() string { return e.Msg }
