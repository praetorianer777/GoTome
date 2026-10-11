// Package koplugin is GOtome's plugin for KOReader (#197), which fetches
// the books sent to a device, offered as a zip from the profile page. Its
// Lua is embedded here so that the binary can hand it out; its logic
// outside KOReader is tested in LuaJIT (make test-koplugin).
package koplugin

import (
	"archive/zip"
	"embed"
	"io"
	"io/fs"
	"regexp"
)

//go:embed all:gotome.koplugin
var plugin embed.FS

// Folder is the plugin's folder in the zip, which goes into KOReader's
// plugins folder as it is.
const Folder = "gotome.koplugin"

// origin is a server address a plugin may be told: a scheme, a host and a
// port, nothing a Lua string would have to escape.
var origin = regexp.MustCompile(`^https?://[A-Za-z0-9.\-]+(:\d+)?$|^https?://\[[0-9A-Fa-f:.]+\](:\d+)?$`)

// Zip writes the plugin as a zip. server, the address the browser used,
// becomes the plugin's default server where it is a plain address.
func Zip(w io.Writer, server string) error {
	z := zip.NewWriter(w)
	err := fs.WalkDir(plugin, Folder, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := plugin.ReadFile(path)
		if err != nil {
			return err
		}
		f, err := z.Create(path)
		if err != nil {
			return err
		}
		_, err = f.Write(data)
		return err
	})
	if err != nil {
		return err
	}
	if origin.MatchString(server) {
		f, err := z.Create(Folder + "/gotome_server.lua")
		if err != nil {
			return err
		}
		if _, err := io.WriteString(f, "-- Where this copy of the plugin was downloaded from.\nreturn \""+server+"\"\n"); err != nil {
			return err
		}
	}
	return z.Close()
}
