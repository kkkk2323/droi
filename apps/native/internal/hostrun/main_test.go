package hostrun

import "testing"

func TestHostArgsTakeAPort(t *testing.T) {
	for _, tc := range []struct {
		args []string
		port int
		help bool
		bad  bool
	}{
		{args: nil},
		{args: []string{"--port", "41418"}, port: 41418},
		{args: []string{"--port=41418"}, port: 41418},
		{args: []string{"-h"}, help: true},
		{args: []string{"--help"}, help: true},
		{args: []string{"--port"}, bad: true},
		{args: []string{"--port", "0"}, bad: true},
		{args: []string{"--port", "65536"}, bad: true},
		{args: []string{"--port", "x"}, bad: true},
		{args: []string{"serve"}, bad: true},
	} {
		port, help, err := parseArgs(tc.args)
		if (err != nil) != tc.bad || port != tc.port || help != tc.help {
			t.Errorf("parseArgs(%q) = %d, %v, %v; want %d, %v, error %v", tc.args, port, help, err, tc.port, tc.help, tc.bad)
		}
	}
}
