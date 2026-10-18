// Package vulnlib exists only so the fixture can require a module that the
// test vulnerability database (testdata/vulndb) lists. It is not real.
package vulnlib

func Version() string { return "1.0.0" }
