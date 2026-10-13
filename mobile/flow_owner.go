package mobile

// FlowOwner looks up the originating Android UID; -1 means unknown.
// local is the app socket, remote is its destination, both numeric IPv4 addresses.
type FlowOwner interface {
	Owner(protocol int64, local string, localPort int64, remote string, remotePort int64) int64
}
