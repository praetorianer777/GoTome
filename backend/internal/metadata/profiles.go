package metadata

// shippedProfiles are tried in order; the first that fits a library is
// its starting score until it has learned its own.
var shippedProfiles = []shipped{}
