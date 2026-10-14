package hostrun

import "path/filepath"

// ConfigDir holds the Host's settings, Memory and logs: $XDG_CONFIG_HOME/droi,
// ~/.config/droi by default.
func ConfigDir(env func(string) string, home string) string {
	return filepath.Join(xdgDir(env, "XDG_CONFIG_HOME", home, ".config"), "droi")
}

// TrashDir is the freedesktop Trash: $XDG_DATA_HOME/Trash.
func TrashDir(env func(string) string, home string) string {
	return filepath.Join(xdgDir(env, "XDG_DATA_HOME", home, filepath.Join(".local", "share")), "Trash")
}

// xdgDir is the variable's folder, or home/fallback; the specification
// says to ignore a relative path.
func xdgDir(env func(string) string, name, home, fallback string) string {
	if d := env(name); filepath.IsAbs(d) {
		return d
	}
	return filepath.Join(home, fallback)
}
