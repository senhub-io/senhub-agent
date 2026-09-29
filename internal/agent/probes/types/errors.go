package types

import "errors"

// ErrCredentialsRejected marks a probe start that failed because the
// target refused the credentials. The agent does not retry such a start
// on its timer: every retry is another failed sign-on, and some targets
// disable the account after a few (IBM i with QMAXSIGN, Active Directory
// lockout policies). The start is tried again when the configuration is
// reloaded or the agent restarts. Wrap it: fmt.Errorf("...: %w", ErrCredentialsRejected).
var ErrCredentialsRejected = errors.New("credentials rejected by the target")
