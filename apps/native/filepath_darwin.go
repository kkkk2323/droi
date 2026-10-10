package main

import (
	"github.com/ebitengine/purego/objc"
)

// filePath asks Foundation for the path of a file reference (/.file/id=…),
// "" when the file is gone.
func filePath(ref string) string {
	url := objc.ID(objc.GetClass("NSURL")).Send(objc.RegisterName("URLWithString:"), nsString("file://"+ref))
	path := url.Send(objc.RegisterName("filePathURL")).Send(objc.RegisterName("path"))
	if path == 0 {
		return ""
	}
	return objc.Send[string](path, objc.RegisterName("UTF8String"))
}

func nsString(s string) objc.ID {
	return objc.ID(objc.GetClass("NSString")).Send(objc.RegisterName("stringWithUTF8String:"), s)
}
