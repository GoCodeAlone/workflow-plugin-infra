package dnspolicy

import "errors"

var (
	ErrMultipleDefaults = errors.New("dnspolicy: multiple RRs set d=true")
	ErrEmptyOwner       = errors.New("dnspolicy: o= field is empty")
	ErrUnknownHeritage  = errors.New("dnspolicy: unknown heritage value (parser ignored RR)")
)

const HeritageV1 = "wfinfra-v1"
