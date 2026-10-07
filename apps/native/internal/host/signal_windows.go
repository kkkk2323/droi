package host

import "os"

func terminateSignal() os.Signal { return os.Kill }
