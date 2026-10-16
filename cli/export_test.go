package cli

// ResolveBuildInfo exposes resolveBuildInfo to the external test package, so build info resolution is tested
// without assigning to Version, Revision or Time.
var ResolveBuildInfo = resolveBuildInfo
