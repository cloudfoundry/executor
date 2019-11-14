# package systemcerts

**Note**: This repository should be imported as `code.cloudfoundry.org/systemcerts`.

This repo is a copy of the Go1.13 `crypto/x509` package. This code has been modified to implement the SystemCertPool on Windows, that was reverted in this [commit](https://github.com/golang/go/commit/2c8b70eacfc3fd2d86bd8e4e4764f11a2e9b3deb) due to
some edge cases that don't really apply to BOSH-deployed jobs (see [Issue #18609](https://github.com/golang/go/issues/18609) for details). These edge cases are expected to be fixed in a future version of Golang.
