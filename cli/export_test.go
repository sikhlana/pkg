package cli

// ResolveBuildInfo exposes resolveBuildInfo to the external test package, so build info resolution is tested
// without assigning to Version, Revision or Time.
var ResolveBuildInfo = resolveBuildInfo

// Run exposes run to the external test package, so Main's behaviour up to os.Exit is tested in the test process.
var Run = run
