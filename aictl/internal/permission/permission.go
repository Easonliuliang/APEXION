package permission

import "encoding/json"

// Decision represents the outcome of a permission check.
type Decision int

const (
	Allow            Decision = iota // Automatically allowed
	Deny                             // Denied
	NeedConfirmation                 // Requires user confirmation
)

// Policy checks whether a tool call should be allowed.
type Policy interface {
	Check(toolName string, params json.RawMessage) Decision
}
