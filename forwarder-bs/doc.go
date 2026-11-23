// Package forwarderbs provides a Basic Station CUPS/LNS proxy and analytics
// collection. Construct with New, then use Run for an owned listener or Start,
// Handler, TLSConfig and Shutdown with an application-owned HTTP server.
// Persistence implementations must perform revision checks atomically.
// FilesystemPersistence supports one owning process per directory.
package forwarderbs
