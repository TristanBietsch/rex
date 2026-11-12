//go:build windows

package main

// killOrphan is unused on Windows (no hot restart).
func killOrphan(int) {}
