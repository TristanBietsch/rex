package state

import (
	"os"
	"testing"
)

// chunkSize matches pty supervisor read buffer (supervisor.go).
const benchChunkSize = 4096

func benchChunk() []byte {
	b := make([]byte, benchChunkSize)
	for i := range b {
		b[i] = byte('a' + (i % 26))
	}
	return b
}

func BenchmarkTranscriptFile_4KiB(b *testing.B) {
	dir := b.TempDir()
	tf, err := OpenTranscript(dir, "bench-sess")
	if err != nil {
		b.Fatal(err)
	}
	defer tf.Close()
	chunk := benchChunk()
	b.SetBytes(int64(len(chunk)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := tf.Write(chunk); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkTranscriptFile_1MiB(b *testing.B) {
	dir := b.TempDir()
	chunk := benchChunk()
	const chunks = (1 << 20) / benchChunkSize
	b.SetBytes(1 << 20)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tf, err := OpenTranscript(dir, "bench-sess")
		if err != nil {
			b.Fatal(err)
		}
		for c := 0; c < chunks; c++ {
			if err := tf.Write(chunk); err != nil {
				_ = tf.Close()
				b.Fatal(err)
			}
		}
		if err := tf.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkAppendTranscript_4KiB(b *testing.B) {
	dir := b.TempDir()
	chunk := benchChunk()
	b.SetBytes(int64(len(chunk)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := AppendTranscript(dir, "bench-sess", chunk); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkAppendTranscript_1MiB(b *testing.B) {
	dir := b.TempDir()
	chunk := benchChunk()
	const chunks = (1 << 20) / benchChunkSize
	b.SetBytes(1 << 20)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = os.RemoveAll(sessionDir(dir, "bench-sess"))
		for c := 0; c < chunks; c++ {
			if err := AppendTranscript(dir, "bench-sess", chunk); err != nil {
				b.Fatal(err)
			}
		}
	}
}
