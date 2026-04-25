package cache

import "os"

// mkdirAll is a thin wrapper kept here so the SQLite backend doesn't
// import the rest of the package's exported symbols.
func mkdirAll(dir string, mode os.FileMode) error {
	return os.MkdirAll(dir, mode)
}
