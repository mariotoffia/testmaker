package shared

// Redistributable is the reuse gate for a source's items and the provenance an
// item inherits from it: `yes` (ship items as-is), `conditional` (license terms
// apply — attribution, share-alike), `no` (mirror the format only, never the
// items). It lives in the shared kernel because it starts life on a source but
// travels onto every item derived from that source, and the item context cannot
// import the source context. The single most load-bearing reuse field.
type Redistributable string

const (
	RedistYes         Redistributable = "yes"
	RedistConditional Redistributable = "conditional"
	RedistNo          Redistributable = "no"
)

// Valid reports whether the value is a known tri-state.
func (r Redistributable) Valid() bool {
	return r == RedistYes || r == RedistConditional || r == RedistNo
}
