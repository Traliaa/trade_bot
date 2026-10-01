//go:build darwin || linux

package main

import (
	"io"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestExitCLIRejectsFIFOWithoutWaitingForWriter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "input.json")
	if err := syscall.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- run([]string{"-input", path}, io.Discard, io.Discard) }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("accepted a nonregular input")
		}
	case <-time.After(time.Second):
		// Unblock the old implementation so the failing test leaves no reader.
		f, err := os.OpenFile(path, os.O_RDWR|syscall.O_NONBLOCK, 0600)
		if err != nil {
			t.Fatal(err)
		}
		f.Close()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("reader remained blocked after cleanup")
		}
		t.Fatal("nonregular input blocked waiting for a writer")
	}
}
