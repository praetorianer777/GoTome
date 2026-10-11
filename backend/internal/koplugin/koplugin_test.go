package koplugin

import (
	"archive/zip"
	"bytes"
	"io"
	"testing"
)

func files(t *testing.T, server string) map[string]string {
	t.Helper()
	var buf bytes.Buffer
	if err := Zip(&buf, server); err != nil {
		t.Fatal(err)
	}
	r, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, f := range r.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(rc)
		rc.Close()
		out[f.Name] = string(data)
	}
	return out
}

func TestTheZipHoldsThePluginAndWhereItCameFrom(t *testing.T) {
	got := files(t, "https://books.example.org:8443")
	for _, name := range []string{"_meta.lua", "main.lua", "gotome_feed.lua"} {
		if got["gotome.koplugin/"+name] == "" {
			t.Errorf("no %s in %v", name, got)
		}
	}
	if s := got["gotome.koplugin/gotome_server.lua"]; s != "-- Where this copy of the plugin was downloaded from.\nreturn \"https://books.example.org:8443\"\n" {
		t.Errorf("server: %q", s)
	}
	for _, server := range []string{`http://evil"..os.exit()..".org`, "javascript:alert(1)", ""} {
		if _, ok := files(t, server)["gotome.koplugin/gotome_server.lua"]; ok {
			t.Errorf("a plugin told %q", server)
		}
	}
}
