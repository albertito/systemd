package systemd_test

import (
	"fmt"
	"net"

	"blitiri.com.ar/go/systemd"
)

func serve(l net.Listener) {
	// Serve over the listener.
}

func Example() {
	listeners, err := systemd.Listeners()
	if err != nil {
		fmt.Printf("error getting listeners: %v", err)
		return
	}

	// Iterate over the listeners of a particular systemd socket.
	// The name comes from the FileDescriptorName option, defaults to the name
	// of the socket unit.
	for _, l := range listeners["service.socket"] {
		go serve(l)
	}
}
