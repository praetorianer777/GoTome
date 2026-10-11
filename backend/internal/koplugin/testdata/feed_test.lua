-- The plugin's logic outside KOReader: luajit testdata/feed_test.lua, from
-- the package's folder (make test-koplugin).
package.path = "gotome.koplugin/?.lua;" .. package.path
local feed = require("gotome_feed")

local failures = 0
local function check(name, got, want)
    if got ~= want then
        failures = failures + 1
        io.stderr:write(("FAIL %s: got %q, want %q\n"):format(name, tostring(got), tostring(want)))
    end
end

local file = assert(io.open("testdata/device-feed.xml"))
local entries = feed.entries(file:read("*a"))
file:close()

check("entries", #entries, 2)
check("title", entries[1].title, "Pride & Prejudice: A Novel")
check("id", entries[1].id, "urn:uuid:01a12b2a-1800-7548-b533-dd0bff7d11e5")
check("links", #entries[1].links, 2)
check("best first", entries[1].links[1].href, "/api/v1/opds/device/files/01a12b2a-1800-7548-b533-dd0bff7d11e6")
check("extension from the format", feed.extension(entries[1].links[1]), "epub")
check("extension of a Kindle file", feed.extension(entries[2].links[1]), "azw3")
check("extension from the type", feed.extension({ type = "application/pdf" }), "pdf")

check("file name", feed.file_name(entries[1].title, "epub"), "Pride & Prejudice_ A Novel.epub")
check("no folders", feed.file_name("../../etc/passwd", "epub"), "_.._etc_passwd.epub")
check("nothing left", feed.file_name(" . ", "pdf"), "book.pdf")
local long = feed.file_name(string.rep("ä", 50), "epub")
check("long names cut at a character", long, string.rep("ä", 40) .. ".epub")

local taken = { ["/books/Emma.epub"] = true, ["/books/Emma (2).epub"] = true }
check("free path", feed.free_path("/books", "Emma.epub", function(p) return taken[p] end), "/books/Emma (3).epub")
check("free path at once", feed.free_path("/books", "Dune.epub", function(p) return taken[p] end), "/books/Dune.epub")

check("absolute", feed.absolute("https://books.example.org/gotome", "/api/v1/opds/device/files/1"),
    "https://books.example.org/api/v1/opds/device/files/1")
check("absolute with a port", feed.absolute("http://nas:25560", "/x"), "http://nas:25560/x")
check("feed", feed.feed_url("https://books.example.org/"), "https://books.example.org/api/v1/opds/device")
check("feed from the catalogue's address", feed.feed_url("http://nas:25560/api/v1/opds"), "http://nas:25560/api/v1/opds/device")

-- The rest runs only in KOReader, but must at least be Lua it reads.
for _, path in ipairs({ "gotome.koplugin/main.lua", "gotome.koplugin/_meta.lua" }) do
    local chunk, err = loadfile(path)
    check("compiles " .. path, chunk ~= nil, true)
    if err then io.stderr:write(err .. "\n") end
end

if failures > 0 then
    os.exit(1)
end
print("ok: the plugin's feed, file names and addresses")
