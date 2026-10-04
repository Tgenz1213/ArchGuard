package output

type SkippedFile struct {
	File   string
	Reason string
}

type FailedCheck struct {
	File   string
	ADRID  string
	Title  string
	Reason string
}
