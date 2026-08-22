// Package docscoverage holds a test, and no production code.
//
// It answers one question the rest of the suite cannot: does every
// configuration key the agent parses appear in the reference
// documentation? Nothing else notices when a parser and a page drift
// apart, because they drift by omission — a key is added, it works, and
// the page describing its block is never opened again.
//
// The failure that motivated it (#816) was `governance`: parsed, in use
// on our own hosts, and present in no reference page at all. It stamps
// facts the agent cannot discover — ownership, criticality, location —
// so documentation is the ONLY way anyone learns the option exists. An
// undocumented key of that kind is a feature that ships and is never
// used.
//
// The check is deliberately one-directional. It proves a parsed key is
// mentioned somewhere in the user guide; it does not prove the mention
// is correct, complete, or on the right page. That is a reviewer's job.
// What it does buy is that nobody can add a key and ship it invisible.
package docscoverage
