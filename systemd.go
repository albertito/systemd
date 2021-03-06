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

// Keep a single global map of files/listeners, to avoid repeatedly parsing
// which can be problematic (see parse).
var files map[string][]*os.File
var listeners map[string][]net.Listener
var parseError error
var listenError error
var mutex sync.Mutex

// parse files, updating the global state.
// This function messes with file descriptors and environment, so it is not
// idempotent and must be called only once. For the callers' convenience, we
// save the files and listener maps globally, and reuse them on the
// user-visible functions.
func parse() {
	mutex.Lock()
	defer mutex.Unlock()

	if files != nil {
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

	// If LISTEN_FDNAMES is set at all, it should have as many names as we
	// have descriptors. If it isn't set, then we map them all to "".
	if fdNamesStr == "" {
		fdNames = []string{}
		for i := 0; i < nfds; i++ {
			fdNames = append(fdNames, "")
		}
	} else {
		if nfds > 0 && len(fdNames) != nfds {
			parseError = fmt.Errorf(
				"Incorrect LISTEN_FDNAMES, have you set FileDescriptorName?")
			return
		}
	}

	files = map[string][]*os.File{}
	listeners = map[string][]net.Listener{}

	for i := 0; i < nfds; i++ {
		fd := firstFD + i
		// We don't want childs to inherit these file descriptors.
		syscall.CloseOnExec(fd)

		name := fdNames[i]

		sysName := fmt.Sprintf("[systemd-fd-%d-%v]", fd, name)
		f := os.NewFile(uintptr(fd), sysName)
		files[name] = append(files[name], f)

		// Note this can fail for non-TCP listeners, so we put the error in a
		// separate variable.
		lis, err := net.FileListener(f)
		if err != nil {
			listenError = fmt.Errorf(
				"Error making listener out of fd %d: %v", fd, err)
		} else {
			listeners[name] = append(listeners[name], lis)
		}
	}

	// Remove them from the environment, to prevent accidental reuse (by
	// us or children processes).
	os.Unsetenv("LISTEN_PID")
	os.Unsetenv("LISTEN_FDS")
	os.Unsetenv("LISTEN_FDNAMES")
}

// Listeners returns net.Listeners corresponding to the file descriptors
// passed by systemd via environment variables.
//
// It returns a map of the form (file descriptor name -> []net.Listener).
//
// The file descriptor name comes from the "FileDescriptorName=" option in the
// systemd socket unit. Multiple socket units can have the same name, hence
// the slice of listeners for each name.
//
// If the "FileDescriptorName=" option is not used, then all file descriptors
// are mapped to the "" name.
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
	if parseError != nil {
		return listeners, parseError
	}
	return listeners, listenError
}

// OneListener returns a net.Listener for the first systemd socket with the
// given name. If there are none, the listener and error will both be nil. An
// error will be returned only if there were issues parsing the file
// descriptors.
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
	if listenError != nil {
		return nil, listenError
	}

	lis := listeners[name]
	if len(lis) < 1 {
		return nil, nil
	}
	return lis[0], nil
}

// Listen returns a net.Listener for the given address, similar to net.Listen.
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

// Files returns the open files passed by systemd via environment variables.
//
// It returns a map of the form (file descriptor name -> []*os.File).
//
// The file descriptor name comes from the "FileDescriptorName=" option in the
// systemd socket unit. Multiple socket units can have the same name, hence
// the slice of listeners for each name.
//
// If the "FileDescriptorName=" option is not used, then all file descriptors
// are mapped to the "" name.
//
// Ideally you should not need to call this more than once. If you do, the
// same files will be returned, as repeated calls to this function will return
// the same results: the parsing is done only once, and the results are saved
// and reused.
//
// See sd_listen_fds(3) and sd_listen_fds_with_names(3) for more details on
// how the passing works.
//
// Normally you would use Listeners instead; however, access to the file
// descriptors can be useful if you need more fine-grained control over
// listener creation, for example if you need to create packet connections
// from them.
func Files() (map[string][]*os.File, error) {
	parse()
	return files, parseError
}
