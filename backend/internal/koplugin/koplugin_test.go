package koplugin

import (
	"archive/zip"
	"bytes"
	"io"
	"testing"
)

func files(t *testing.T, server string, addresses []string) map[string]string {
	t.Helper()
	var buf bytes.Buffer
	if err := Zip(&buf, server, addresses); err != nil {
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
	got := files(t, "https://books.example.org:8443", nil)
	for _, name := range []string{"_meta.lua", "main.lua", "gotome_feed.lua", "gotome_annotations.lua"} {
		if got["gotome.koplugin/"+name] == "" {
			t.Errorf("no %s in %v", name, got)
		}
	}
	if s := got["gotome.koplugin/gotome_server.lua"]; s != "-- Where this copy of the plugin was downloaded from, and the other addresses of the server.\nreturn { \"https://books.example.org:8443\" }\n" {
		t.Errorf("server: %q", s)
	}
	for _, server := range []string{`http://evil"..os.exit()..".org`, "javascript:alert(1)", ""} {
		if _, ok := files(t, server, nil)["gotome.koplugin/gotome_server.lua"]; ok {
			t.Errorf("a plugin told %q", server)
		}
	}
}

func TestTheZipCarriesTheConfiguredAddressesWithTheBrowsersFirst(t *testing.T) {
	const head = "-- Where this copy of the plugin was downloaded from, and the other addresses of the server.\nreturn "
	home, road := "http://192.168.1.5:8080", "https://books.example.org"
	got := files(t, "http://nas.local:8080", []string{home, road})["gotome.koplugin/gotome_server.lua"]
	if want := head + `{ "http://nas.local:8080", "http://192.168.1.5:8080", "https://books.example.org" }` + "\n"; got != want {
		t.Errorf("server: %q", got)
	}
	got = files(t, road, []string{home, road})["gotome.koplugin/gotome_server.lua"]
	if want := head + `{ "http://192.168.1.5:8080", "https://books.example.org" }` + "\n"; got != want {
		t.Errorf("server when the browser's is listed: %q", got)
	}
	got = files(t, "", []string{home, `http://evil"..os.exit()..".org`})["gotome.koplugin/gotome_server.lua"]
	if want := head + `{ "http://192.168.1.5:8080" }` + "\n"; got != want {
		t.Errorf("server with a bad address: %q", got)
	}
}
