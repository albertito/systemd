// Package systemd implements a way to get network listeners from systemd,
// similar to C's sd_listen_fds(3) and sd_listen_fds_with_names(3).
package systemd // import "blitiri.com.ar/go/systemd"

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
)

var (
	// Error to return when $LISTEN_PID does not refer to us.
	ErrPIDMismatch = errors.New("$LISTEN_PID != our PID")

	// First FD for listeners.
	// It's 3 by definition, but using a variable simplifies testing.
	firstFD = 3
)

// Keep a single global map of listeners, to avoid repeatedly parsing which
// can be problematic (see parse).
var listeners map[string][]net.Listener
var parseError error
var mutex sync.Mutex

// parse listeners, updating the global state.
// This function messes with file descriptors and environment, so it is not
// idempotent and must be called only once. For the callers' convenience, we
// save the listeners map globally and reuse it on the user-visible functions.
func parse() {
	mutex.Lock()
	defer mutex.Unlock()

	if listeners != nil {
		return
	}

	pidStr := os.Getenv("LISTEN_PID")
	nfdsStr := os.Getenv("LISTEN_FDS")
	fdNamesStr := os.Getenv("LISTEN_FDNAMES")
	fdNames := strings.Split(fdNamesStr, ":")

	// Nothing to do if the variables are not set.
	if pidStr == "" || nfdsStr == "" {
		return
	}

	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		parseError = fmt.Errorf(
			"error converting $LISTEN_PID=%q: %v", pidStr, err)
		return
	} else if pid != os.Getpid() {
		parseError = ErrPIDMismatch
		return
	}

	nfds, err := strconv.Atoi(os.Getenv("LISTEN_FDS"))
	if err != nil {
		parseError = fmt.Errorf(
			"error reading $LISTEN_FDS=%q: %v", nfdsStr, err)
		return
	}

	// We should have as many names as we have descriptors.
	// Note that if we have no descriptors, fdNames will be [""] (due to how
	// strings.Split works), so we consider that special case.
	if nfds > 0 && (fdNamesStr == "" || len(fdNames) != nfds) {
		parseError = fmt.Errorf(
			"Incorrect LISTEN_FDNAMES, have you set FileDescriptorName?")
		return
	}

	listeners = map[string][]net.Listener{}

	for i := 0; i < nfds; i++ {
		fd := firstFD + i
		// We don't want childs to inherit these file descriptors.
		syscall.CloseOnExec(fd)

		name := fdNames[i]

		sysName := fmt.Sprintf("[systemd-fd-%d-%v]", fd, name)
		lis, err := net.FileListener(os.NewFile(uintptr(fd), sysName))
		if err != nil {
			parseError = fmt.Errorf(
				"Error making listener out of fd %d: %v", fd, err)
			return
		}

		listeners[name] = append(listeners[name], lis)
	}

	// Remove them from the environment, to prevent accidental reuse (by
	// us or children processes).
	os.Unsetenv("LISTEN_PID")
	os.Unsetenv("LISTEN_FDS")
	os.Unsetenv("LISTEN_FDNAMES")
}

// Listeners returns a map of listeners for the file descriptors passed by
// systemd via environment variables.
//
// It returns a map of the form (file descriptor name -> slice of listeners).
//
// The file descriptor name comes from the "FileDescriptorName=" option in the
// systemd socket unit. Multiple socket units can have the same name, hence
// the slice of listeners for each name.
//
// Ideally you should not need to call this more than once. If you do, the
// same listeners will be returned, as repeated calls to this function will
// return the same results: the parsing is done only once, and the results are
// saved and reused.
//
// See sd_listen_fds(3) and sd_listen_fds_with_names(3) for more details on
// how the passing works.
func Listeners() (map[string][]net.Listener, error) {
	parse()
	return listeners, parseError
}

// OneListener returns a listener for the first systemd socket with the given
// name. If there are none, the listener and error will both be nil. An error
// will be returned only if there were issues parsing the file descriptors.
//
// This function can be convenient for simple callers where you know there's
// only one file descriptor being passed with the given name.
//
// This is a convenience function built on top of Listeners().
func OneListener(name string) (net.Listener, error) {
	parse()
	if parseError != nil {
		return nil, parseError
	}

	lis := listeners[name]
	if len(lis) < 1 {
		return nil, nil
	}
	return lis[0], nil
}

// Listen returns a listener for the given address, similar to net.Listen.
//
// If the address begins with "&" it is interpreted as a systemd socket being
// passed.  For example, using "&http" would mean we expect a systemd socket
// passed to us, named with "FileDescriptorName=http" in its unit.
//
// Otherwise, it uses net.Listen to create a new listener with the given net
// and local address.
//
// This function can be convenient for simple callers where you get the
// address from a user, and want to let them specify either "use systemd" or a
// normal address without too much additional complexity.
//
// This is a convenience function built on top of Listeners().
func Listen(netw, laddr string) (net.Listener, error) {
	if strings.HasPrefix(laddr, "&") {
		name := laddr[1:]
		lis, err := OneListener(name)
		if lis == nil && err == nil {
			err = fmt.Errorf("systemd socket %q not found", name)
		}
		return lis, err
	} else {
		return net.Listen(netw, laddr)
	}
}
