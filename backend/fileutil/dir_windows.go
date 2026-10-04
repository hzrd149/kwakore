//go:build windows

package fileutil

// syncDir is a no-op: Windows does not support fsync on a directory handle,
// and MoveFileEx (os.Rename) is already durable enough for our purposes.
func syncDir(dir string) error { return nil }

// TightenDir is a no-op: the data dir lives under %AppData%, which already
// carries a user-only ACL.
func TightenDir(dir string) error { return nil }
