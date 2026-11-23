//go:build darwin && !cgo

package privatefs

import "os"

func ClearInheritedACL(*os.File) error { return os.ErrPermission }
func NoExtendedACLFile(*os.File) bool  { return false }
func NoExtendedACL(string) bool        { return false }
