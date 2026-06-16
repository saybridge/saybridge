package domain

// SystemActorID is the UUID used for system-initiated actions (bot messages, plugin operations, etc.).
// This replaces scattered "00000000-0000-0000-0000-000000000000" hardcoded strings.
const SystemActorID = "00000000-0000-0000-0000-000000000000"

// SystemActorName is the display name for system-initiated messages.
const SystemActorName = "System"

// DefaultTenantID is used when multi-tenancy is not yet configured.
const DefaultTenantID = "default"
