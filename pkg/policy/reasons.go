package policy

// UnhandledReason identifies why a flow was not converted to a policy rule.
// The FlowTracker interface accepts values of this type so call sites cannot
// drift into raw string literals that bypass the enumerated set below.
type UnhandledReason string

const (
	ReasonNoL4            UnhandledReason = "no_l4"
	ReasonNilSource       UnhandledReason = "nil_source"
	ReasonNilDestination  UnhandledReason = "nil_destination"
	ReasonNilEndpoint     UnhandledReason = "nil_endpoint"
	ReasonEmptyNamespace  UnhandledReason = "empty_namespace"
	ReasonUnknownProtocol UnhandledReason = "unknown_protocol"
	ReasonWorldNoIP       UnhandledReason = "world_no_ip"
	ReasonReservedID      UnhandledReason = "reserved_identity"
	ReasonUnknownDir      UnhandledReason = "unknown_direction"
)
