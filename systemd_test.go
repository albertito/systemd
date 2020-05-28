package systemd

import (
	"fmt"
	"math/rand"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
)

// setenv prepares the environment and resets the internal state.
func setenv(pid, fds string, names ...string) {
	os.Setenv("LISTEN_PID", pid)
	os.Setenv("LISTEN_FDS", fds)
	os.Setenv("LISTEN_FDNAMES", strings.Join(names, ":"))
	files = nil
	listeners = nil
	parseError = nil
	listenError = nil
}

// newListener creates a TCP listener.
func newListener(t *testing.T) *net.TCPListener {
	t.Helper()
	addr := &net.TCPAddr{
		Port: 0,
	}

	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		t.Fatalf("Could not create TCP listener: %v", err)
	}

	return l
}

// listenerFd returns a file descriptor for the listener.
// Note it is a NEW file descriptor, not the original one.
func listenerFd(t *testing.T, l *net.TCPListener) int {
	t.Helper()
	f, err := l.File()
	if err != nil {
		t.Fatalf("Could not get TCP listener file: %v", err)
	}

	return int(f.Fd())
}

func sameAddr(a, b net.Addr) bool {
	return a.Network() == b.Network() && a.String() == b.String()
}

func TestEmptyEnvironment(t *testing.T) {
	cases := []struct{ pid, fds string }{
		{"", ""},
		{"123", ""},
		{"", "4"},
	}
	for _, c := range cases {
		setenv(c.pid, c.fds)

		if ls, err := Listeners(); ls != nil || err != nil {
			t.Logf("Case: LISTEN_PID=%q  LISTEN_FDS=%q", c.pid, c.fds)
			t.Errorf("Unexpected result: %v // %v", ls, err)
		}

		if fs, err := Files(); fs != nil || err != nil {
			t.Logf("Case: LISTEN_PID=%q  LISTEN_FDS=%q", c.pid, c.fds)
			t.Errorf("Unexpected result: %v // %v", fs, err)
		}
	}
}

func TestBadEnvironment(t *testing.T) {
	// Create a listener so we have something to reference.
	l := newListener(t)
	defer l.Close()
	firstFD = listenerFd(t, l)

	ourPID := strconv.Itoa(os.Getpid())
	cases := []struct {
		pid, fds string
		names    []string
	}{
		{"a", "1", []string{"name"}},              // Invalid PID.
		{ourPID, "a", []string{"name"}},           // Invalid number of fds.
		{"1", "1", []string{"name"}},              // PID != ourselves.
		{ourPID, "1", []string{"name1", "name2"}}, // Too many names.
	}
	for _, c := range cases {
		setenv(c.pid, c.fds, c.names...)

		if ls, err := Listeners(); err == nil {
			t.Logf("Case: LISTEN_PID=%q  LISTEN_FDS=%q LISTEN_FDNAMES=%q",
				c.pid, c.fds, c.names)
			t.Errorf("Unexpected result: %v // %v", ls, err)
		}
		if ls, err := OneListener("name"); err == nil {
			t.Errorf("Unexpected result: %v // %v", ls, err)
		}
		if fs, err := Files(); err == nil {
			t.Logf("Case: LISTEN_PID=%q  LISTEN_FDS=%q LISTEN_FDNAMES=%q",
				c.pid, c.fds, c.names)
			t.Errorf("Unexpected result: %v // %v", fs, err)
		}
	}
}

func TestWrongPID(t *testing.T) {
	// Find a pid != us. 1 should always work in practice.
	pid := 1
	for pid == os.Getpid() {
		pid = rand.Int()
	}

	setenv(strconv.Itoa(pid), "4")
	if _, err := Listeners(); err != ErrPIDMismatch {
		t.Errorf("Did not fail with PID mismatch: %v", err)
	}

	if _, err := OneListener("name"); err != ErrPIDMismatch {
		t.Errorf("Did not fail with PID mismatch: %v", err)
	}

	if _, err := Files(); err != ErrPIDMismatch {
		t.Errorf("Did not fail with PID mismatch: %v", err)
	}
}

func TestNoFDs(t *testing.T) {
	setenv(strconv.Itoa(os.Getpid()), "0")
	if ls, err := Listeners(); len(ls) != 0 || err != nil {
		t.Errorf("Got a non-empty result: %v // %v", ls, err)
	}

	if ls, err := Files(); len(ls) != 0 || err != nil {
		t.Errorf("Got a non-empty result: %v // %v", ls, err)
	}
	if l, err := OneListener("nothing"); l != nil || err != nil {
		t.Errorf("Unexpected result: %v // %v", l, err)
	}
	if l, err := Listen("tcp", "&nothing"); l != nil || err == nil {
		t.Errorf("Unexpected result: %v // %v", l, err)
	}
}

func TestBadFDs(t *testing.T) {
	f, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatalf("Failed to open /dev/null: %v", err)
	}
	defer f.Close()

	setenv(strconv.Itoa(os.Getpid()), "1")
	firstFD = int(f.Fd())

	if ls, err := Listeners(); len(ls) != 0 || err == nil {
		t.Errorf("Got a non-empty result: %v // %v", ls, err)
	}

	if l, err := OneListener(""); l != nil || err == nil {
		t.Errorf("Got a non-empty result: %v // %v", l, err)
	}

	// It's not a bad FD as far as Files() is concerned.
	fs, err := Files()
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if len(fs) != 1 || len(fs[""]) != 1 {
		t.Errorf("Unexpected result: %v", fs)
	}
	if got := fs[""][0]; got.Fd() != f.Fd() {
		t.Errorf("File descriptor %d != expected %d (%v)",
			got.Fd(), f.Fd(), got)
	}
}

func TestOneSocket(t *testing.T) {
	l := newListener(t)
	defer l.Close()
	firstFD = listenerFd(t, l)

	setenv(strconv.Itoa(os.Getpid()), "1", "name")

	{
		lsMap, err := Listeners()
		if err != nil || len(lsMap) != 1 {
			t.Fatalf("Got an invalid result: %v // %v", lsMap, err)
		}

		ls := lsMap["name"]
		if !sameAddr(ls[0].Addr(), l.Addr()) {
			t.Errorf("Listener 0 address mismatch, expected %#v, got %#v",
				l.Addr(), ls[0].Addr())
		}

		oneL, err := OneListener("name")
		if err != nil {
			t.Errorf("OneListener error: %v", err)
		}
		if !sameAddr(oneL.Addr(), l.Addr()) {
			t.Errorf("OneListener address mismatch, expected %#v, got %#v",
				l.Addr(), ls[0].Addr())
		}
	}

	{
		fsMap, err := Files()
		if err != nil || len(fsMap) != 1 || len(fsMap["name"]) != 1 {
			t.Fatalf("Got an invalid result: %v // %v", fsMap, err)
		}

		f := fsMap["name"][0]
		flis, err := net.FileListener(f)
		if err != nil {
			t.Errorf("File was not a listener: %v", err)
		}
		if !sameAddr(flis.Addr(), l.Addr()) {
			t.Errorf("Listener 0 address mismatch, expected %#v, got %#v",
				l.Addr(), flis.Addr())
		}
	}

	if os.Getenv("LISTEN_PID") != "" || os.Getenv("LISTEN_FDS") != "" {
		t.Errorf("Failed to reset the environment")
	}
}

func TestManySockets(t *testing.T) {
	// Create two contiguous listeners.
	// The test environment does not guarantee us that they are contiguous, so
	// keep going until they are.
	var l0, l1 *net.TCPListener
	var f0, f1 int = -1, -3

	for f0+1 != f1 {
		// We have to be careful with the order of these operations, because
		// listenerFd will create *new* file descriptors.
		l0 = newListener(t)
		l1 = newListener(t)
		f0 = listenerFd(t, l0)
		f1 = listenerFd(t, l1)
		defer l0.Close()
		defer l1.Close()
		t.Logf("Looping for FDs: %d %d", f0, f1)
	}

	expected := []*net.TCPListener{l0, l1}

	firstFD = f0

	setenv(strconv.Itoa(os.Getpid()), "2", "name1", "name2")

	{
		lsMap, err := Listeners()
		if err != nil || len(lsMap) != 2 {
			t.Fatalf("Got an invalid result: %v // %v", lsMap, err)
		}

		ls := []net.Listener{
			lsMap["name1"][0],
			lsMap["name2"][0],
		}

		for i := 0; i < 2; i++ {
			if !sameAddr(ls[i].Addr(), expected[i].Addr()) {
				t.Errorf("Listener %d address mismatch, expected %#v, got %#v",
					i, ls[i].Addr(), expected[i].Addr())
			}
		}

		oneL, _ := OneListener("name1")
		if !sameAddr(oneL.Addr(), expected[0].Addr()) {
			t.Errorf("OneListener address mismatch, expected %#v, got %#v",
				oneL.Addr(), expected[0].Addr())
		}
	}

	{
		fsMap, err := Files()
		if err != nil || len(fsMap) != 2 {
			t.Fatalf("Got an invalid result: %v // %v", fsMap, err)
		}

		for i := 0; i < 2; i++ {
			name := fmt.Sprintf("name%d", i+1)
			fs := fsMap[name]
			if len(fs) != 1 {
				t.Errorf("fsMap[%q] = %v had %d entries, expected 1",
					name, fs, len(fs))
			}

			flis, err := net.FileListener(fs[0])
			if err != nil {
				t.Errorf("File was not a listener: %v", err)
			}
			if !sameAddr(flis.Addr(), expected[i].Addr()) {
				t.Errorf("Listener %d address mismatch, expected %#v, got %#v",
					i, flis.Addr(), expected[i].Addr())
			}
		}
	}

	if os.Getenv("LISTEN_PID") != "" ||
		os.Getenv("LISTEN_FDS") != "" ||
		os.Getenv("LISTEN_FDNAMES") != "" {
		t.Errorf("Failed to reset the environment")
	}

	// Test that things also work with LISTEN_FDNAMES unset.
	setenv(strconv.Itoa(os.Getpid()), "2")
	os.Unsetenv("LISTEN_FDNAMES")
	{
		lsMap, err := Listeners()
		if err != nil || len(lsMap) != 1 || len(lsMap[""]) != 2 {
			t.Fatalf("Got an invalid result: %v // %v", lsMap, err)
		}

		ls := []net.Listener{
			lsMap[""][0],
			lsMap[""][1],
		}

		for i := 0; i < 2; i++ {
			if !sameAddr(ls[i].Addr(), expected[i].Addr()) {
				t.Errorf("Listener %d address mismatch, expected %#v, got %#v",
					i, ls[i].Addr(), expected[i].Addr())
			}
		}
	}

}

func TestListen(t *testing.T) {
	orig := newListener(t)
	defer orig.Close()
	firstFD = listenerFd(t, orig)
	setenv(strconv.Itoa(os.Getpid()), "1", "name")

	l, err := Listen("tcp", "&name")
	if err != nil {
		t.Errorf("Listen failed: %v", err)
	}
	if !sameAddr(l.Addr(), orig.Addr()) {
		t.Errorf("Listener 0 address mismatch, expected %#v, got %#v",
			l.Addr(), orig.Addr())
	}

	l, err = Listen("tcp", ":0")
	if err != nil {
		t.Errorf("Listen failed: %v", err)
	}
	t.Logf("listener created at %v", l.Addr())
}
