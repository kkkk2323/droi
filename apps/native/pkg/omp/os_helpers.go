package omp

import "os"

func osMkdirAll(dir string) error { return os.MkdirAll(dir, 0o700) }

func osWriteFile(path string, b []byte) error { return os.WriteFile(path, b, 0o600) }
