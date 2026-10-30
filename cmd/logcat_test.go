package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func writeVia(t *testing.T, path string, tee bool, console *bytes.Buffer, text string) {
	t.Helper()
	w, closeFn, err := logWriter(path, tee, console)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(text)); err != nil {
		t.Fatal(err)
	}
	if err := closeFn(); err != nil {
		t.Fatal(err)
	}
}

func TestLogWriterFileAppends(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ios.log")
	var console bytes.Buffer
	writeVia(t, path, false, &console, "one\n")
	writeVia(t, path, false, &console, "two\n")
	if got, _ := os.ReadFile(path); string(got) != "one\ntwo\n" {
		t.Fatalf("file = %q", got)
	}
	if console.Len() != 0 {
		t.Fatalf("console should be untouched, got %q", console.String())
	}
}

func TestLogWriterTee(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ios.log")
	var console bytes.Buffer
	writeVia(t, path, true, &console, "line\n")
	if got, _ := os.ReadFile(path); string(got) != "line\n" || console.String() != "line\n" {
		t.Fatalf("file=%q console=%q", got, console.String())
	}
}

func TestLogWriterConsoleOnlyAndErrors(t *testing.T) {
	var console bytes.Buffer
	writeVia(t, "", false, &console, "x")
	if console.String() != "x" {
		t.Fatalf("console = %q", console.String())
	}
	if _, _, err := logWriter("", true, &console); err == nil {
		t.Fatal("--tee without --output must fail")
	}
	if _, _, err := logWriter(filepath.Join(t.TempDir(), "missing", "a.log"), false, &console); err == nil {
		t.Fatal("missing directory must fail")
	}
}
