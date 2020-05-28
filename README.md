
# blitiri.com.ar/go/systemd

[![GoDoc](https://godoc.org/blitiri.com.ar/go/systemd?status.svg)](https://godoc.org/blitiri.com.ar/go/systemd)
[![Build Status](https://travis-ci.org/albertito/systemd.svg?branch=master)](https://travis-ci.org/albertito/systemd)

[systemd](https://godoc.org/blitiri.com.ar/go/systemd) is a Go package
implementing a way to get network listeners from systemd, similar
to C's `sd_listen_fds()` and `sd_listen_fds_with_names()`
([man](https://www.freedesktop.org/software/systemd/man/sd_listen_fds.html)).

Supports named file descriptors, which is useful if your daemon needs to be
able to tell the different ports apart (e.g. http vs https).

It is used by daemons such as [chasquid](https://blitiri.com.ar/p/chasquid/)
to listen on privileged ports without needing to run as root.


## Example

```go
listeners, err := systemd.Listeners()
for _, l := range listeners["service.socket"] {
	go serve(l)
}
```


## Status

The API should be considered stable, and no backwards-incompatible changes are
expected.


## Contact

If you have any questions, comments or patches please send them to
albertito@blitiri.com.ar.


