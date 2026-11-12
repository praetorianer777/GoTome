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

-- The server's own answer, which TestTheDeviceClientsFixturesAreTheServersAnswers
-- holds the file to (make device-fixtures writes it anew).
local file = assert(io.open("testdata/device-feed.xml"))
local entries = feed.entries(file:read("*a"))
file:close()

check("entries", #entries, 2)
check("title", entries[1].title, "Pride & Prejudice: A Novel")
check("id", entries[1].id, "urn:uuid:0192f0a0-0000-7000-8000-000000000001")
check("links", #entries[1].links, 2)
check("best first", entries[1].links[1].href, "/api/v1/opds/device/files/0192f0a0-0000-7000-8000-000000000002")
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
check("free path at once", feed.free_path("/books", "Dusk.epub", function(p) return taken[p] end), "/books/Dusk.epub")

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

local home, road = "http://192.168.1.5:8080", "https://books.example.org"
local function joined(list) return table.concat(list, " ") end
check("a string is one address", joined(feed.candidates(nil, home)), home)
check("own address first, none twice", joined(feed.candidates(road .. "/", { home, road })), road .. "/ " .. home)
check("the last good first", joined(feed.candidates(nil, { home, road }, road)), road .. " " .. home)
check("a last good that is gone is ignored", joined(feed.candidates(nil, { home, road }, "http://gone")), home .. " " .. road)
check("nothing named", #feed.candidates(nil, nil), 0)

local tried = {}
local function answers(...)
    local up = {}
    for _i, a in ipairs({ ... }) do up[a] = true end
    tried = {}
    return function(a) table.insert(tried, a) return up[a] == true end
end
local chosen, count = feed.choose({ home, road }, answers(road))
check("falls back to the second", chosen, road)
check("having tried two", count, 2)
check("tried in order", joined(tried), home .. " " .. road)
chosen, count = feed.choose({ home, road }, answers(home, road))
check("stops at the first that answers", joined(tried), home)
check("the first", chosen, home)
chosen, count = feed.choose({ home, road }, answers())
check("none answers", chosen, nil)
check("having tried all", count, 2)

if failures > 0 then
    os.exit(1)
end
print("ok: the plugin's feed, file names and addresses")
